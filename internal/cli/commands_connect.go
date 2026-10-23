package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/store"
)

func cmdConnect(args []string) error {
	fs := newFlagSet("connect", `usage: cdp connect --session <name> --port <port> <url>
or:    cdp connect --session <name> --port <port> --tab <index|id|pattern>
or:    cdp connect --session <name> --port <port> --new [<url>]

Without --new, <url> (or --url <url>) matches an existing tab: exact URL
(case-insensitive), then prefix, then substring; the first match wins.
With --new, open <url> (default about:blank); --url and --new-url are aliases.
Quote URLs containing shell punctuation such as '&'. Different URLs conflict;
--tab cannot be combined with a URL or --new.`)
	sessionFlag := addSessionFlag(fs)
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", portDefault(0), "DevTools port")
	var urls []connectURL
	fs.Func("url", "Existing tab URL to match, or URL to open with --new", func(value string) error {
		urls = append(urls, connectURL{"--url", value})
		return nil
	})
	targetRef := fs.String("tab", "", "Tab index, id, or pattern from tabs list")
	newTab := fs.Bool("new", false, "Open a new tab and connect to it")
	fs.Func("new-url", "Alias for the URL to open with --new (default about:blank)", func(value string) error {
		urls = append(urls, connectURL{"--new-url", value})
		return nil
	})
	activate := fs.Bool("activate", true, "Activate the tab after opening (with --new)")
	timeout := fs.Duration("timeout", 5*time.Second, "Connection timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	targetURL, err := resolveConnectURL(pos, urls, *newTab, *targetRef)
	if err != nil {
		return err
	}
	name, err := resolveSessionName(*sessionFlag)
	if err != nil {
		fs.Usage()
		return err
	}
	if *port == 0 {
		return errors.New("--port is required")
	}
	st, err := store.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var target cdp.TargetInfo
	switch {
	case *newTab:
		tab, err := cdp.CreateTarget(ctx, *host, *port, targetURL)
		if err != nil {
			return err
		}
		if latest, latestErr := fetchLatestTargetInfo(ctx, *host, *port, tab); latestErr == nil {
			tab = latest
		}
		if tab.URL == "" {
			tab.URL = targetURL
		}
		if *activate {
			if err := cdp.ActivateTarget(ctx, *host, *port, tab.ID); err != nil {
				return err
			}
		}
		target = tab
	case *targetRef != "":
		tabs, err := fetchTabs(ctx, *host, *port)
		if err != nil {
			return fmt.Errorf("list tabs failed (check with 'cdp tabs list --host %s --port %d'): %w", *host, *port, err)
		}
		if len(tabs) == 0 {
			return fmt.Errorf("no tabs available (run 'cdp tabs list --host %s --port %d' to confirm)", *host, *port)
		}
		tab, err := matchTab(tabs, *targetRef)
		if err != nil {
			return err
		}
		target = tab
	default:
		targets, err := cdp.ListTargets(ctx, *host, *port)
		if err != nil {
			return fmt.Errorf("list targets failed (check with 'cdp tabs list --host %s --port %d'): %w", *host, *port, err)
		}
		found, ok := cdp.FindTarget(targets, targetURL)
		if !ok {
			return fmt.Errorf("no target matching %s (run 'cdp tabs list --host %s --port %d' to confirm)", targetURL, *host, *port)
		}
		target = found
	}
	if target.WebSocket == "" {
		return errors.New("target does not expose webSocketDebuggerUrl")
	}
	wsURL := rewriteWebSocketURL(target.WebSocket, *host, *port)

	client, err := cdp.Dial(ctx, wsURL)
	if err != nil {
		return err
	}
	defer client.Close()

	if _, err := client.Evaluate(ctx, "document.readyState"); err != nil {
		return fmt.Errorf("tab handshake failed: %w", err)
	}
	issue, _ := detectPageIssue(ctx, wsURL)
	if latest, latestErr := fetchLatestTargetInfo(ctx, *host, *port, target); latestErr == nil {
		target = latest
	}

	session := store.Session{
		Name:           name,
		Host:           *host,
		Port:           *port,
		URL:            target.URL,
		TargetID:       target.ID,
		WebSocketURL:   wsURL,
		Title:          target.Title,
		Type:           target.Type,
		LastConnected:  time.Now(),
		LastTargetInfo: target.Description,
	}
	if err := st.Set(session); err != nil {
		return err
	}
	fmt.Printf("Connected %s -> %s (%s)%s\n", name, formatTargetTitle(target), target.URL, formatPageIssueSuffix(issue))
	return nil
}

type connectURL struct {
	source string
	value  string
}

// Resolve aliases before contacting the browser. Keep every supplied URL so a
// repeated flag cannot silently replace a different target.
func resolveConnectURL(pos []string, urls []connectURL, newTab bool, targetRef string) (string, error) {
	if len(pos) > 1 {
		return "", errors.New("connect accepts one positional URL; use --new <url> to open it or <url> to match an existing tab")
	}
	if len(pos) == 1 {
		if strings.HasPrefix(pos[0], "-") {
			return "", fmt.Errorf("unexpected argument: %s (see 'cdp connect --help')", pos[0])
		}
		urls = append(urls, connectURL{"positional URL", pos[0]})
	}
	if newTab && targetRef != "" {
		return "", errors.New("--new cannot be combined with --tab; use --new <url> to open a tab or --tab <index|id|pattern> to select an existing tab")
	}
	for _, u := range urls {
		if u.source == "--new-url" && !newTab {
			return "", errors.New("--new-url requires --new; use --new <url> to open a tab or --url <url> to match an existing tab")
		}
		if strings.TrimSpace(u.value) == "" {
			return "", fmt.Errorf("%s must not be empty; use --new without a URL to open about:blank", u.source)
		}
		if u.value != urls[0].value {
			return "", fmt.Errorf("conflicting URLs: %s %q and %s %q; supply one URL (or repeat the same URL)", urls[0].source, urls[0].value, u.source, u.value)
		}
	}
	if targetRef != "" && len(urls) > 0 {
		return "", errors.New("--tab cannot be combined with a URL; use either --tab <index|id|pattern> or a URL to select an existing tab")
	}
	if len(urls) > 0 {
		return urls[0].value, nil
	}
	if newTab {
		return "about:blank", nil
	}
	if targetRef == "" {
		return "", errors.New("a URL, --tab <index|id|pattern>, or --new [<url>] is required")
	}
	return "", nil
}

func cmdKeepAlive(args []string) error {
	fs := newFlagSet("keep-alive", "usage: cdp keep-alive --session <name>")
	sessionFlag := addSessionFlag(fs)
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if err := unexpectedArgs(pos); err != nil {
		return err
	}
	name, err := resolveSessionName(*sessionFlag)
	if err != nil {
		fs.Usage()
		return err
	}

	st, err := store.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	handle, err := openSession(ctx, st, name)
	if err != nil {
		return err
	}
	defer handle.Close()

	commands := []struct {
		method string
		params map[string]interface{}
	}{
		{"Emulation.setFocusEmulationEnabled", map[string]interface{}{"enabled": true}},
		{"Page.setWebLifecycleState", map[string]interface{}{"state": "active"}},
		{"Page.bringToFront", nil},
	}
	for _, cmd := range commands {
		if err := handle.client.Call(ctx, cmd.method, cmd.params, nil); err != nil {
			return err
		}
	}
	fmt.Printf("Keep-alive applied to %s (%s)\n", name, abbreviate(handle.session.Title, 60))
	return nil
}

func cmdDisconnect(args []string) error {
	fs := newFlagSet("disconnect", "usage: cdp disconnect --session <name>")
	sessionFlag := addSessionFlag(fs)
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if err := unexpectedArgs(pos); err != nil {
		return err
	}
	name, err := resolveSessionName(*sessionFlag)
	if err != nil {
		fs.Usage()
		return err
	}

	st, err := store.Load()
	if err != nil {
		return err
	}
	if _, ok := st.Get(name); !ok {
		return fmt.Errorf("unknown session %q", name)
	}
	if _, err := st.Remove(name); err != nil {
		return err
	}
	fmt.Printf("Disconnected session %s (tab left open)\n", name)
	return nil
}
