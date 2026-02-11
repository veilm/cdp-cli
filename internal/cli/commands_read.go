package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/veilm/cdp-cli/internal/store"
)

func cmdRead(args []string) error {
	fs := newFlagSet("read", "usage: cdp read --session <name> [options] [selector...]")
	sessionFlag := addSessionFlag(fs)
	jsonOut := fs.Bool("json", false, "Output JSON instead of text")
	waitMs := fs.Int("wait-ms", 0, "Extra wait before parsing (ms)")
	waitReady := fs.Bool("wait", false, "Wait for document.readyState == 'complete' before reading")
	hasText := fs.String("has-text", "", "Only include elements whose subtree text matches this text/regex")
	attValue := fs.String("att-value", "", "Only include elements whose attribute values match this text/regex")
	classLimit := fs.Int("class-limit", 3, "Max number of classes to include in element labels")
	timeout := fs.Duration("timeout", 10*time.Second, "Command timeout")

	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}

	sessionName, err := resolveSessionName(*sessionFlag)
	if err != nil {
		fs.Usage()
		return err
	}

	selector := strings.TrimSpace(strings.Join(pos, " "))
	if selector != "" {
		selector = normalizeSelector(selector)
	}
	if *classLimit < 0 {
		return errors.New("--class-limit must be >= 0")
	}
	if *waitMs < 0 {
		return errors.New("--wait-ms must be >= 0")
	}

	st, err := store.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	handle, err := openSession(ctx, st, sessionName)
	if err != nil {
		return err
	}
	defer handle.Close()

	if *waitReady {
		if err := waitForReadyState(ctx, handle.client, 200*time.Millisecond); err != nil {
			return err
		}
	}

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	opts := map[string]interface{}{
		"waitMs": *waitMs,
		"rootSelector": func() interface{} {
			if selector == "" {
				return nil
			}
			return selector
		}(),
		"hasText":    *hasText,
		"attValue":   *attValue,
		"classLimit": *classLimit,
	}
	optsJSON, _ := json.Marshal(opts)

	expression := fmt.Sprintf("window.WebNavRead(%s)", string(optsJSON))
	// Use the "by reference" eval path (returnByValue=false) since read results can be
	// large and some Chromium builds are flaky about returning them by value.
	raw, err := handle.client.EvaluateRaw(ctx, expression, false)
	if err != nil {
		return err
	}
	value, err := handle.client.RemoteObjectValue(ctx, raw.Result)
	if err != nil {
		return err
	}
	m, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected WebNavRead result type %T", value)
	}
	url, _ := m["url"].(string)
	title, _ := m["title"].(string)

	linesAny, _ := m["lines"].([]interface{})
	lines := make([]string, 0, len(linesAny))
	for _, v := range linesAny {
		if s, ok := v.(string); ok {
			lines = append(lines, s)
		} else if v != nil {
			lines = append(lines, fmt.Sprint(v))
		}
	}

	payload := struct {
		URL   string   `json:"url"`
		Title string   `json:"title"`
		Lines []string `json:"lines"`
	}{URL: url, Title: title, Lines: lines}

	if *jsonOut {
		pretty, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(pretty))
		return nil
	}

	if len(lines) == 0 && title != "" {
		fmt.Println(strings.TrimSpace(title))
		return nil
	}
	out := strings.Join(lines, "\n")
	fmt.Print(out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Print("\n")
	}
	return nil
}

func normalizeSelector(selector string) string {
	if selector == "" {
		return selector
	}
	// Assume backslashes are only attempts to escape selectors; normalize to one \ before each /.
	selector = strings.ReplaceAll(selector, `\`, "")
	selector = strings.ReplaceAll(selector, `/`, `\/`)
	return selector
}
