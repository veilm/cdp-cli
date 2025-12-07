package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/format"
	"github.com/veilm/cdp-cli/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		printUsage()
		return nil
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "help", "--help", "-h":
		printUsage()
		return nil
	case "connect":
		return cmdConnect(args)
	case "eval":
		return cmdEval(args)
	case "dom":
		return cmdDOM(args)
	case "styles":
		return cmdStyles(args)
	case "rect":
		return cmdRect(args)
	case "screenshot":
		return cmdScreenshot(args)
	case "log":
		return cmdLog(args)
	case "targets":
		return cmdTargets(args)
	case "close":
		return cmdClose(args)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage() {
	fmt.Println(`cdp - Chrome DevTools CLI helper

Usage:
  cdp connect <name> --port 9222 --url https://example
	  cdp eval <name> "JS expression" [--pretty] [--depth N]
	  cdp dom <name> "CSS selector"
	  cdp styles <name> "CSS selector"
	  cdp rect <name> "CSS selector"
	  cdp screenshot <name> [--selector ".composer"] [--output file.png]
	  cdp log <name> ["script to eval before streaming"]
	  cdp targets
  cdp close <name>`)
}

func cmdConnect(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cdp connect <name> --port --url")
	}
	name := args[0]

	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", 0, "DevTools port")
	targetURL := fs.String("url", "", "Tab URL to bind to")
	timeout := fs.Duration("timeout", 5*time.Second, "Connection timeout")
	fs.Parse(args[1:])
	if *port == 0 {
		return errors.New("--port is required")
	}
	if *targetURL == "" {
		return errors.New("--url is required")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}

	st, err := store.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	targets, err := cdp.ListTargets(ctx, *host, *port)
	if err != nil {
		return err
	}
	target, ok := cdp.FindTarget(targets, *targetURL)
	if !ok {
		return fmt.Errorf("no target matching %s", *targetURL)
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

func cmdEval(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: cdp eval <name> \"expr\"")
	}
	name := args[0]

	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	pretty := fs.Bool("pretty", defaultPretty(), "Pretty print JSON output")
	depth := fs.Int("depth", -1, "Max depth before truncating (-1 = unlimited)")
	timeout := fs.Duration("timeout", 10*time.Second, "Eval timeout")
	file := fs.String("file", "", "Read JS from file path ('-' for stdin)")
	readStdin := fs.Bool("stdin", false, "Read JS from stdin")
	fs.Parse(args[1:])

	filePath := *file
	useStdin := *readStdin
	if filePath == "-" {
		if useStdin {
			return errors.New("use either --file or --stdin, not both")
		}
		useStdin = true
		filePath = ""
	}
	if useStdin && filePath != "" {
		return errors.New("use either --file or --stdin, not both")
	}

	var expression string
	switch {
	case filePath != "":
		src, err := readScriptFile(filePath)
		if err != nil {
			return err
		}
		expression = src
	case useStdin:
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		expression = string(src)
	default:
		if fs.NArg() == 0 {
			return errors.New("missing JS expression (pass literal, --file, or --stdin)")
		}
		expression = fs.Arg(0)
		if fs.NArg() > 1 {
			return fmt.Errorf("unexpected argument: %s", fs.Arg(1))
		}
	}
	if strings.TrimSpace(expression) == "" {
		return errors.New("JS expression is empty")
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

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	output, err := format.JSON(value, *pretty, *depth)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func cmdDOM(args []string) error {
	fs := flag.NewFlagSet("dom", flag.ExitOnError)
	pretty := fs.Bool("pretty", true, "Pretty print output")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) < 2 {
		return errors.New("usage: cdp dom <name> \".selector\"")
	}
	name := args[0]
	selector := args[1]
	fs.Parse(args[2:])
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
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

	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) { return null; }
        return {
            outerHTML: el.outerHTML,
            text: el.innerText,
        };
    })()`, strconv.Quote(selector))

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	if value == nil {
		fmt.Println("null")
		return nil
	}
	output, err := format.JSON(value, *pretty, -1)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func cmdStyles(args []string) error {
	fs := flag.NewFlagSet("styles", flag.ExitOnError)
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) < 2 {
		return errors.New("usage: cdp styles <name> \".selector\"")
	}
	name := args[0]
	selector := args[1]
	fs.Parse(args[2:])
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
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

	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) { return null; }
        const computed = window.getComputedStyle(el);
        const rect = el.getBoundingClientRect();
        const interesting = [
            'display','position','top','left','right','bottom','width','height',
            'marginTop','marginRight','marginBottom','marginLeft',
            'paddingTop','paddingRight','paddingBottom','paddingLeft',
            'borderTopWidth','borderRightWidth','borderBottomWidth','borderLeftWidth',
            'fontSize','fontWeight','lineHeight','color','backgroundColor'
        ];
        const styles = {};
        for (const key of interesting) {
            styles[key] = computed.getPropertyValue(key);
        }
        return {
            styles,
            box: {
                top: rect.top,
                left: rect.left,
                right: rect.right,
                bottom: rect.bottom,
                width: rect.width,
                height: rect.height,
            }
        };
    })()`, strconv.Quote(selector))

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	output, err := format.JSON(value, true, -1)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func cmdRect(args []string) error {
	fs := flag.NewFlagSet("rect", flag.ExitOnError)
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) < 2 {
		return errors.New("usage: cdp rect <name> \".selector\"")
	}
	name := args[0]
	selector := args[1]
	fs.Parse(args[2:])
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
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

	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) { return null; }
        const rect = el.getBoundingClientRect();
        return {
            x: rect.x,
            y: rect.y,
            top: rect.top,
            left: rect.left,
            right: rect.right,
            bottom: rect.bottom,
            width: rect.width,
            height: rect.height,
        };
    })()`, strconv.Quote(selector))

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	output, err := format.JSON(value, true, -1)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func cmdScreenshot(args []string) error {
	fs := flag.NewFlagSet("screenshot", flag.ExitOnError)
	selector := fs.String("selector", "", "CSS selector to crop")
	output := fs.String("output", "screenshot.png", "Output file path")
	timeout := fs.Duration("timeout", 15*time.Second, "Command timeout")
	if len(args) < 1 {
		return errors.New("usage: cdp screenshot <name> [--selector ...]")
	}
	name := args[0]
	fs.Parse(args[1:])
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
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

	params := map[string]interface{}{
		"format":                "png",
		"captureBeyondViewport": true,
	}

	if *selector != "" {
		clip, err := resolveClip(ctx, handle.client, *selector)
		if err != nil {
			return err
		}
		if clip == nil {
			return fmt.Errorf("selector %s not found", *selector)
		}
		params["clip"] = clip
	}

	var shot struct {
		Data string `json:"data"`
	}
	if err := handle.client.Call(ctx, "Page.captureScreenshot", params, &shot); err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("Saved %s (%d bytes)\n", *output, len(data))
	return nil
}

func resolveClip(ctx context.Context, client *cdp.Client, selector string) (map[string]interface{}, error) {
	if err := client.Call(ctx, "DOM.enable", nil, nil); err != nil {
		return nil, err
	}
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := client.Call(ctx, "DOM.getDocument", map[string]interface{}{"depth": -1, "pierce": true}, &doc); err != nil {
		return nil, err
	}
	var node struct {
		NodeID int `json:"nodeId"`
	}
	if err := client.Call(ctx, "DOM.querySelector", map[string]interface{}{
		"nodeId":   doc.Root.NodeID,
		"selector": selector,
	}, &node); err != nil {
		return nil, err
	}
	if node.NodeID == 0 {
		return nil, nil
	}
	var box struct {
		Model struct {
			Width   float64   `json:"width"`
			Height  float64   `json:"height"`
			Content []float64 `json:"content"`
		} `json:"model"`
	}
	if err := client.Call(ctx, "DOM.getBoxModel", map[string]interface{}{"nodeId": node.NodeID}, &box); err != nil {
		return nil, err
	}
	if len(box.Model.Content) < 2 {
		return nil, errors.New("element has no box model")
	}
	clip := map[string]interface{}{
		"x":      box.Model.Content[0],
		"y":      box.Model.Content[1],
		"width":  box.Model.Width,
		"height": box.Model.Height,
		"scale":  1,
	}
	return clip, nil
}

func cmdLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("usage: cdp log <name> [\"setup script\"]")
	}
	name := fs.Arg(0)
	script := ""
	if fs.NArg() > 1 {
		script = fs.Arg(1)
	}

	st, err := store.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle, err := openSession(ctx, st, name)
	if err != nil {
		return err
	}
	defer handle.Close()

	if err := handle.client.Call(ctx, "Runtime.enable", nil, nil); err != nil {
		return err
	}
	if err := handle.client.Call(ctx, "Log.enable", nil, nil); err != nil {
		return err
	}

	events := make(chan cdp.Event, 64)
	unsubscribe := handle.client.SubscribeEvents(func(evt cdp.Event) {
		select {
		case events <- evt:
		default:
		}
	})
	defer unsubscribe()

	if script != "" {
		if _, err := handle.client.Evaluate(ctx, script); err != nil {
			return fmt.Errorf("setup script failed: %w", err)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	fmt.Println("Streaming console output. Ctrl+C to stop...")

	go func() {
		<-sigCh
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt := <-events:
			if err := handleLogEvent(ctx, handle.client, evt); err != nil {
				fmt.Fprintln(os.Stderr, "log handler:", err)
			}
		}
	}
}

func handleLogEvent(ctx context.Context, client *cdp.Client, evt cdp.Event) error {
	switch evt.Method {
	case "Runtime.consoleAPICalled":
		var payload struct {
			Type string             `json:"type"`
			Args []cdp.RemoteObject `json:"args"`
		}
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return err
		}
		values := make([]string, 0, len(payload.Args))
		for _, arg := range payload.Args {
			val, err := client.RemoteObjectValue(ctx, arg)
			if err != nil {
				values = append(values, fmt.Sprintf("<error: %v>", err))
				continue
			}
			switch t := val.(type) {
			case string:
				values = append(values, t)
			default:
				out, err := format.JSON(t, false, 2)
				if err != nil {
					values = append(values, fmt.Sprintf("%v", t))
				} else {
					values = append(values, out)
				}
			}
		}
		fmt.Printf("[%s] %s\n", payload.Type, strings.Join(values, " "))
	case "Log.entryAdded":
		var payload struct {
			Entry struct {
				Source string `json:"source"`
				Level  string `json:"level"`
				Text   string `json:"text"`
				URL    string `json:"url"`
				Line   int    `json:"lineNumber"`
				Column int    `json:"columnNumber"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return err
		}
		entry := payload.Entry
		location := ""
		if entry.URL != "" {
			location = fmt.Sprintf(" (%s:%d:%d)", entry.URL, entry.Line, entry.Column)
		}
		fmt.Printf("[%s/%s] %s%s\n", entry.Source, entry.Level, entry.Text, location)
	}
	return nil
}

func cmdTargets(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: cdp targets")
	}
	st, err := store.Load()
	if err != nil {
		return err
	}
	sessions := st.List()
	if len(sessions) == 0 {
		fmt.Println("No saved sessions")
		return nil
	}
	names := make([]string, 0, len(sessions))
	for name := range sessions {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("%-12s %-6s %-30s %s\n", "NAME", "PORT", "TITLE", "URL")
	for _, name := range names {
		session := sessions[name]
		fmt.Printf("%-12s %-6d %-30s %s\n", name, session.Port, abbreviate(session.Title, 30), session.URL)
	}
	return nil
}

func abbreviate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}

func cmdClose(args []string) error {
	fs := flag.NewFlagSet("close", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: cdp close <name>")
	}
	name := fs.Arg(0)

	st, err := store.Load()
	if err != nil {
		return err
	}
	session, ok := st.Get(name)
	if !ok {
		return fmt.Errorf("unknown session %q", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, updated, err := attachSession(ctx, session)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Call(ctx, "Target.closeTarget", map[string]interface{}{"targetId": updated.TargetID}, nil); err != nil {
		return err
	}
	if _, err := st.Remove(name); err != nil {
		return err
	}
	fmt.Println("Closed session", name)
	return nil
}

func openSession(ctx context.Context, st *store.Store, name string) (*sessionHandle, error) {
	session, ok := st.Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown session %q", name)
	}
	client, updated, err := attachSession(ctx, session)
	if err != nil {
		return nil, err
	}
	return &sessionHandle{client: client, store: st, session: updated, persist: true}, nil
}

func attachSession(ctx context.Context, session store.Session) (*cdp.Client, store.Session, error) {
	client, err := cdp.Dial(ctx, session.WebSocketURL)
	if err == nil {
		return client, session, nil
	}
	targets, listErr := cdp.ListTargets(ctx, session.Host, session.Port)
	if listErr != nil {
		return nil, session, fmt.Errorf("connect failed (%v) and retry listing targets failed: %w", err, listErr)
	}
	var target cdp.TargetInfo
	found := false
	for _, t := range targets {
		if t.ID == session.TargetID {
			target = t
			found = true
			break
		}
	}
	if !found && session.URL != "" {
		if t, ok := cdp.FindTarget(targets, session.URL); ok {
			target = t
			found = true
		}
	}
	if !found {
		return nil, session, fmt.Errorf("target %s is no longer available", session.URL)
	}
	wsURL := rewriteWebSocketURL(target.WebSocket, session.Host, session.Port)
	client, err = cdp.Dial(ctx, wsURL)
	if err != nil {
		return nil, session, err
	}
	session.WebSocketURL = wsURL
	session.TargetID = target.ID
	session.URL = target.URL
	session.Title = target.Title
	session.Type = target.Type
	session.LastTargetInfo = target.Description
	return client, session, nil
}

type sessionHandle struct {
	client  *cdp.Client
	store   *store.Store
	session store.Session
	persist bool
}

func (h *sessionHandle) Close() {
	h.client.Close()
	if !h.persist {
		return
	}
	h.session.LastConnected = time.Now()
	if err := h.store.Set(h.session); err != nil {
		fmt.Fprintln(os.Stderr, "warning: unable to update session:", err)
	}
}

func rewriteWebSocketURL(raw, host string, port int) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "" {
		u.Scheme = "ws"
	}
	if host != "" && port != 0 {
		u.Host = fmt.Sprintf("%s:%d", host, port)
	}
	return u.String()
}

func defaultPretty() bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("CDP_PRETTY")))
	switch val {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func readScriptFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}
