package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TargetInfo mirrors /json/list entries.
type TargetInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	WebSocket   string `json:"webSocketDebuggerUrl"`
	Description string `json:"description"`
}

// ListTargets fetches targets exposed on the DevTools port.
func ListTargets(ctx context.Context, host string, port int) ([]TargetInfo, error) {
	endpoint := fmt.Sprintf("http://%s:%d/json/list", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("list targets: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var targets []TargetInfo
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// FindTarget tries to match a target by URL.
func FindTarget(targets []TargetInfo, rawURL string) (TargetInfo, bool) {
	normalized := strings.TrimSpace(rawURL)
	for _, t := range targets {
		if strings.EqualFold(t.URL, normalized) {
			return t, true
		}
	}
	for _, t := range targets {
		if strings.HasPrefix(t.URL, normalized) || strings.HasPrefix(normalized, t.URL) {
			return t, true
		}
	}
	for _, t := range targets {
		if strings.Contains(t.URL, normalized) {
			return t, true
		}
	}
	return TargetInfo{}, false
}

// ActivateTarget asks the browser to focus a tab.
func ActivateTarget(ctx context.Context, host string, port int, targetID string) error {
	endpoint := fmt.Sprintf("http://%s:%d/json/activate/%s", host, port, targetID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("activate target: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
