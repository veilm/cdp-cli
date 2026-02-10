package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/store"
)

func cmdConnect(args []string) error {
	fs := newFlagSet("connect", "usage: cdp connect --session <name> --port --url\nor:    cdp connect --session <name> --port --tab <index|id|pattern>\nor:    cdp connect --session <name> --port --new [--new-url <url>]")
	sessionFlag := addSessionFlag(fs)
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", portDefault(0), "DevTools port")
	targetURL := fs.String("url", "", "Tab URL to bind to")
	targetRef := fs.String("tab", "", "Tab index, id, or pattern from tabs list")
	newTab := fs.Bool("new", false, "Open a new tab and connect to it")
	newURL := fs.String("new-url", "about:blank", "URL to open when using --new")
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
	if err := unexpectedArgs(pos); err != nil {
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
	if *newTab && (*targetURL != "" || *targetRef != "") {
		return errors.New("use --new without --url or --tab")
	}
	if *targetURL != "" && *targetRef != "" {
		return errors.New("use either --url or --tab, not both")
	}
	if !*newTab && *targetURL == "" && *targetRef == "" {
		return errors.New("one of --url, --tab, or --new is required")
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
		tab, err := cdp.CreateTarget(ctx, *host, *port, *newURL)
		if err != nil {
			return err
		}
		if tab.URL == "" {
			tab.URL = *newURL
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
		found, ok := cdp.FindTarget(targets, *targetURL)
		if !ok {
			return fmt.Errorf("no target matching %s (run 'cdp tabs list --host %s --port %d' to confirm)", *targetURL, *host, *port)
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
	fmt.Printf("Connected %s -> %s (%s)\n", name, target.Title, target.URL)
	return nil
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
