package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

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
	case "wait":
		return cmdWait(args)
	case "wait-visible":
		return cmdWaitVisible(args)
	case "click":
		return cmdClick(args)
	case "hover":
		return cmdHover(args)
	case "drag":
		return cmdDrag(args)
	case "gesture":
		return cmdGesture(args)
	case "key":
		return cmdKey(args)
	case "scroll":
		return cmdScroll(args)
	case "type":
		return cmdType(args)
	case "upload":
		return cmdUpload(args)
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
	case "network-log":
		return cmdNetworkLog(args)
	case "keep-alive":
		return cmdKeepAlive(args)
	case "tabs":
		return cmdTabs(args)
	case "targets":
		return cmdTargets(args)
	case "disconnect":
		return cmdDisconnect(args)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage() {
	fmt.Println("cdp - Chrome DevTools CLI helper")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  cdp connect <name> --port 9222 --url https://example")
	fmt.Println("  \t  cdp connect <name> --port 9222 --tab 3")
	fmt.Println("  \t  cdp connect <name> --port 9222 --new [--new-url https://example]")
	fmt.Println("  \t  cdp eval <name> \"JS expression\" [--pretty] [--depth N] [--json] [--wait]")
	fmt.Println("  \t  cdp wait <name> [--selector \".selector\"] [--visible]")
	fmt.Println("  \t  cdp wait-visible <name> \".selector\"")
	fmt.Println("  \t  cdp click <name> \".selector\" [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
	fmt.Println("  \t  cdp hover <name> \".selector\" [--has-text REGEX] [--att-value REGEX] [--hold DURATION]")
	fmt.Println("  \t  cdp drag <name> \".from\" \".to\" [--from-index N] [--to-index N] [--delay DURATION]")
	fmt.Println("  \t  cdp gesture <name> \".selector\" \"x1,y1 x2,y2 ...\" [--delay DURATION]  (draw, swipe, slide, trace)")
	fmt.Println("  \t  cdp key <name> KEYS [--element \".selector\"] [--cdp]")
	fmt.Println("  \t  cdp scroll <name> <yPx> [--x <xPx>] [--element \".selector\"] [--emit]")
	fmt.Println("  \t  cdp type <name> \".selector\" \"text\" [--has-text REGEX] [--att-value REGEX] [--append]")
	fmt.Println("  \t  cdp upload <name> \"input[type=file]\" <file1> [file2 ...] [--wait]")
	fmt.Println("  \t  cdp dom <name> \"CSS selector\"")
	fmt.Println("  \t  cdp styles <name> \"CSS selector\"")
	fmt.Println("  \t  cdp rect <name> \"CSS selector\"")
	fmt.Println("  \t  cdp screenshot <name> [--selector \".composer\"] [--output file.png] [--full-page] [--cdp-clip]")
	fmt.Println("  \t  cdp log <name> [\"setup script\"] [--level REGEX] [--limit N] [--timeout DURATION]")
	fmt.Println("  \t  cdp network-log <name> [--dir PATH] [--url REGEX] [--method REGEX] [--status REGEX] [--mime REGEX]")
	fmt.Println("  \t  cdp keep-alive <name>")
	fmt.Println("  \t  cdp tabs list [--host 127.0.0.1 --port 9222] [--plain]")
	fmt.Println("  \t  cdp tabs open <url> [--host 127.0.0.1 --port 9222] [--activate=false]")
	fmt.Println("  \t  cdp tabs switch <index|id|pattern> [--host 127.0.0.1 --port 9222]")
	fmt.Println("  \t  cdp tabs close <index|id|pattern> [--host 127.0.0.1 --port 9222]")
	fmt.Println("  \t  cdp targets")
	fmt.Println("  cdp disconnect <name>")
	fmt.Println()
	if port, ok := envDefaultPort(); ok {
		fmt.Printf("Configured default port (CDP_PORT): %d\n\n", port)
	}
	fmt.Println("Run 'cdp <command> --help' for command-specific usage.")

}

func cmdConnect(args []string) error {
	fs := newFlagSet("connect", "usage: cdp connect <name> --port --url\nor:    cdp connect <name> --port --tab <index|id|pattern>\nor:    cdp connect <name> --port --new [--new-url <url>]")
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", portDefault(0), "DevTools port")
	targetURL := fs.String("url", "", "Tab URL to bind to")
	targetRef := fs.String("tab", "", "Tab index, id, or pattern from tabs list")
	newTab := fs.Bool("new", false, "Open a new tab and connect to it")
	newURL := fs.String("new-url", "about:blank", "URL to open when using --new")
	activate := fs.Bool("activate", true, "Activate the tab after opening (with --new)")
	timeout := fs.Duration("timeout", 5*time.Second, "Connection timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp connect <name> --port --url (or --tab)")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errors.New("usage: cdp connect <name> --port --url (or --tab)")
	}
	name := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
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

func cmdEval(args []string) error {
	fs := newFlagSet("eval", "usage: cdp eval <name> \"expr\"")
	pretty := fs.Bool("pretty", defaultPretty(), "Pretty print JSON output")
	depth := fs.Int("depth", -1, "Max depth before truncating (-1 = unlimited)")
	jsonOutput := fs.Bool("json", true, "Serialize objects via JSON.stringify when possible")
	waitReady := fs.Bool("wait", false, "Wait for document.readyState == 'complete' before evaluating")
	timeout := fs.Duration("timeout", 10*time.Second, "Eval timeout")
	file := fs.String("file", "", "Read JS from file path ('-' for stdin)")
	readStdin := fs.Bool("stdin", false, "Read JS from stdin")
	body := fs.Bool("body", false, "Treat input as a function body (wrap in an IIFE and return its value)")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp eval <name> \"expr\"")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errors.New("usage: cdp eval <name> \"expr\"")
	}
	name := pos[0]

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
		if len(pos) < 2 {
			return errors.New("missing JS expression (pass literal, --file, or --stdin)")
		}
		expression = pos[1]
		if len(pos) > 2 {
			return fmt.Errorf("unexpected argument: %s", pos[2])
		}
	}
	if strings.TrimSpace(expression) == "" {
		return errors.New("JS expression is empty")
	}
	bodyInput := expression
	if *body {
		expression = "(function(){\n" + expression + "\n})()"
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

	if *waitReady {
		if err := waitForReadyState(ctx, handle.client, 200*time.Millisecond); err != nil {
			return err
		}
	}

	returnByValue := false
	res, err := handle.client.EvaluateRaw(ctx, expression, returnByValue)
	if err != nil {
		return err
	}
	if returnByValue && res.Result.Subtype == "promise" {
		res, err = handle.client.EvaluateRaw(ctx, expression, false)
		if err != nil {
			return err
		}
	}
	value, err := handle.client.RemoteObjectValue(ctx, res.Result)
	if err != nil {
		return err
	}
	if !*jsonOutput && res.Result.Type == "object" && res.Result.Subtype == "node" {
		fmt.Fprintln(os.Stderr, "warning: eval returned a DOM node; use --json if you want serialized output")
	}
	output, err := format.JSON(value, *pretty, *depth)
	if err != nil {
		return err
	}
	if *body && !containsReturnKeyword(bodyInput) {
		if value == nil {
			fmt.Fprintln(os.Stderr, "warning: the input function body returned undefined; did you forget to include a return statement?")
		} else if s, ok := value.(string); ok && s == "undefined" {
			fmt.Fprintln(os.Stderr, "warning: the input function body returned undefined; did you forget to include a return statement?")
		}
	}
	fmt.Println(output)
	return nil
}

func cmdWait(args []string) error {
	fs := newFlagSet("wait", "usage: cdp wait <name> [--selector \".selector\"] [--visible]")
	selector := fs.String("selector", "", "CSS selector to wait for")
	visible := fs.Bool("visible", false, "Wait for selector to be visible (requires --selector)")
	poll := fs.Duration("poll", 200*time.Millisecond, "Polling interval")
	timeout := fs.Duration("timeout", 10*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp wait <name> [--selector \".selector\"] [--visible]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errors.New("usage: cdp wait <name> [--selector \".selector\"] [--visible]")
	}
	name := pos[0]
	if *visible && *selector == "" {
		return errors.New("--visible requires --selector")
	}
	if *selector != "" {
		if err := rejectUnsupportedSelector(*selector, "wait --selector", false); err != nil {
			return err
		}
	}
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
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

	switch {
	case *selector == "":
		if err := waitForReadyState(ctx, handle.client, *poll); err != nil {
			return err
		}
		fmt.Println("Ready")
	case *visible:
		if err := waitForSelectorVisible(ctx, handle.client, *selector, *poll); err != nil {
			return err
		}
		fmt.Printf("Visible: %s\n", *selector)
	default:
		if err := waitForSelector(ctx, handle.client, *selector, *poll); err != nil {
			return err
		}
		fmt.Printf("Found: %s\n", *selector)
	}
	return nil
}

func containsReturnKeyword(input string) bool {
	// Look for a "return " token preceded by a non-alphanumeric or start-of-string.
	prev := rune(0)
	seenPrev := false
	lowered := strings.ToLower(input)
	for i := 0; i < len(lowered); i++ {
		ch := rune(lowered[i])
		if i+7 <= len(lowered) && lowered[i:i+7] == "return " {
			if !seenPrev || !unicode.IsLetter(prev) && !unicode.IsNumber(prev) {
				return true
			}
		}
		prev = ch
		seenPrev = true
	}
	return false
}

func cmdWaitVisible(args []string) error {
	fs := newFlagSet("wait-visible", "usage: cdp wait-visible <name> \".selector\"")
	poll := fs.Duration("poll", 200*time.Millisecond, "Polling interval")
	timeout := fs.Duration("timeout", 10*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp wait-visible <name> \".selector\"")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp wait-visible <name> \".selector\"")
	}
	name := pos[0]
	selector := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	if err := rejectUnsupportedSelector(selector, "rect", false); err != nil {
		return err
	}
	if err := rejectUnsupportedSelector(selector, "styles", false); err != nil {
		return err
	}
	if err := rejectUnsupportedSelector(selector, "dom", false); err != nil {
		return err
	}
	if err := rejectUnsupportedSelector(selector, "wait-visible", false); err != nil {
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

	if err := waitForSelectorVisible(ctx, handle.client, selector, *poll); err != nil {
		return err
	}
	fmt.Printf("Visible: %s\n", selector)
	return nil
}

func cmdClick(args []string) error {
	fs := newFlagSet("click", "usage: cdp click <name> \".selector\" [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]\n(also supports inline :has-text(...) at the end of the selector)")
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	count := fs.Int("count", 1, "Number of clicks to perform")
	submitWaitMS := fs.Int("submit-wait-ms", 700, "If clicking a submit button inside a form, wait N ms before returning (0 disables)")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp click <name> \".selector\" [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp click <name> \".selector\" [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
	}
	name := pos[0]
	selector := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	selector, inlineHasText, hasInline, err := parseInlineHasText(selector)
	if err != nil {
		return err
	}
	if err := rejectUnsupportedSelector(selector, "click", true); err != nil {
		return err
	}
	if *count < 1 {
		return errors.New("--count must be >= 1")
	}
	selector = autoQuoteAttrValues(selector)
	hasTextValue := *hasText
	if hasInline {
		hasTextValue = inlineHasText
	}
	hasTextValue = escapeLeadingPlusRegexSpec(hasTextValue)
	attValueValue := escapeLeadingPlusRegexSpec(*attValue)

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
        const selector = %s;
        const hasTextSpec = %s;
        const attValueSpec = %s;

        function compileRegex(spec) {
            if (!spec) return null;
            // Support /pattern/flags or plain pattern.
            if (spec.length >= 2 && spec[0] === "/") {
                const last = spec.lastIndexOf("/");
                if (last > 0) {
                    const pattern = spec.slice(1, last);
                    const flags = spec.slice(last + 1);
                    try { return new RegExp(pattern, flags); } catch (e) {}
                }
            }
            try { return new RegExp(spec); } catch (e) {
                throw new Error("invalid regex: " + spec + " (" + (e && e.message ? e.message : e) + ")");
            }
        }

        const hasTextRe = compileRegex(hasTextSpec);
        const attValueRe = compileRegex(attValueSpec);

        function elementText(el) {
            // Prefer textContent when innerText is empty (e.g. display:none).
            return (el && (el.innerText || el.textContent)) || "";
        }

        function attrValueMatches(el, re) {
            const attrs = (el && el.attributes) || null;
            if (!attrs) return false;
            for (let i = 0; i < attrs.length; i++) {
                const v = attrs[i] && attrs[i].value ? attrs[i].value : "";
                if (re.test(v)) return true;
            }
            return false;
        }

        const list = Array.from(document.querySelectorAll(selector));
        let el = null;
        for (let i = 0; i < list.length; i++) {
            const cand = list[i];
            if (hasTextRe && !hasTextRe.test(elementText(cand))) continue;
            if (attValueRe && !attrValueMatches(cand, attValueRe)) continue;
            el = cand;
            break;
        }

        if (!el) {
            throw new Error("no element matched selector/filters: " + selector);
        }
        if (el.scrollIntoView) {
            el.scrollIntoView({block: "center", inline: "center"});
        }
        if (el.focus) {
            el.focus();
        }
        const tag = el.tagName ? el.tagName.toLowerCase() : "";
        let isSubmit = false;
        if (tag === "button") {
            const t = el.getAttribute("type");
            isSubmit = !t || String(t).toLowerCase() === "submit";
        } else if (tag === "input") {
            const t = el.getAttribute("type") || el.type;
            isSubmit = String(t || "").toLowerCase() === "submit";
        }
        const inForm = !!(el.closest && el.closest("form"));
        const clicks = %d;
        for (let i = 0; i < clicks; i++) {
            el.click();
        }
        return { submitForm: isSubmit && inForm };
    })()`, strconv.Quote(selector), strconv.Quote(hasTextValue), strconv.Quote(attValueValue), *count)

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	if *submitWaitMS > 0 {
		if m, ok := value.(map[string]interface{}); ok {
			if submit, _ := m["submitForm"].(bool); submit {
				time.Sleep(time.Duration(*submitWaitMS) * time.Millisecond)
			}
		}
	}
	if *count == 1 {
		fmt.Printf("Clicked: %s\n", selector)
	} else {
		fmt.Printf("Clicked: %s (x%d)\n", selector, *count)
	}
	return nil
}

func cmdHover(args []string) error {
	fs := newFlagSet("hover", "usage: cdp hover <name> \".selector\" [--has-text REGEX] [--att-value REGEX]\n(also supports inline :has-text(...) at the end of the selector)")
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	hold := fs.Duration("hold", 0, "Optional time to wait after hovering")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp hover <name> \".selector\" [--has-text REGEX] [--att-value REGEX]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp hover <name> \".selector\" [--has-text REGEX] [--att-value REGEX]")
	}
	name := pos[0]
	selector := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	selector, inlineHasText, hasInline, err := parseInlineHasText(selector)
	if err != nil {
		return err
	}
	if err := rejectUnsupportedSelector(selector, "hover", true); err != nil {
		return err
	}
	selector = autoQuoteAttrValues(selector)
	hasTextValue := *hasText
	if hasInline {
		hasTextValue = inlineHasText
	}
	hasTextValue = escapeLeadingPlusRegexSpec(hasTextValue)
	attValueValue := escapeLeadingPlusRegexSpec(*attValue)

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
        const selector = %s;
        const hasTextSpec = %s;
        const attValueSpec = %s;

        function compileRegex(spec) {
            if (!spec) return null;
            if (spec.length >= 2 && spec[0] === "/") {
                const last = spec.lastIndexOf("/");
                if (last > 0) {
                    const pattern = spec.slice(1, last);
                    const flags = spec.slice(last + 1);
                    try { return new RegExp(pattern, flags); } catch (e) {}
                }
            }
            try { return new RegExp(spec); } catch (e) {
                throw new Error("invalid regex: " + spec + " (" + (e && e.message ? e.message : e) + ")");
            }
        }

        const hasTextRe = compileRegex(hasTextSpec);
        const attValueRe = compileRegex(attValueSpec);

        function elementText(el) {
            return (el && (el.innerText || el.textContent)) || "";
        }

        function attrValueMatches(el, re) {
            const attrs = (el && el.attributes) || null;
            if (!attrs) return false;
            for (let i = 0; i < attrs.length; i++) {
                const v = attrs[i] && attrs[i].value ? attrs[i].value : "";
                if (re.test(v)) return true;
            }
            return false;
        }

        const list = Array.from(document.querySelectorAll(selector));
        let el = null;
        for (let i = 0; i < list.length; i++) {
            const cand = list[i];
            if (hasTextRe && !hasTextRe.test(elementText(cand))) continue;
            if (attValueRe && !attrValueMatches(cand, attValueRe)) continue;
            el = cand;
            break;
        }
        if (!el) {
            throw new Error("no element matched selector/filters: " + selector);
        }
        if (el.scrollIntoView) {
            el.scrollIntoView({block: "center", inline: "center"});
        }
        if (el.focus) {
            el.focus();
        }
        const rect = el.getBoundingClientRect();
        const x = rect.left + rect.width / 2;
        const y = rect.top + rect.height / 2;

        function dispatchMouse(type) {
            const evt = new MouseEvent(type, {bubbles: true, cancelable: true, clientX: x, clientY: y});
            el.dispatchEvent(evt);
        }

        if (typeof PointerEvent !== "undefined") {
            const pe = (type) => new PointerEvent(type, {bubbles: true, cancelable: true, clientX: x, clientY: y, pointerType: "mouse"});
            el.dispatchEvent(pe("pointerenter"));
            el.dispatchEvent(pe("pointerover"));
            el.dispatchEvent(pe("pointermove"));
        }

        dispatchMouse("mouseenter");
        dispatchMouse("mouseover");
        dispatchMouse("mousemove");
        return {x, y};
    })()`, strconv.Quote(selector), strconv.Quote(hasTextValue), strconv.Quote(attValueValue))

	if _, err := handle.client.Evaluate(ctx, expression); err != nil {
		return err
	}
	if *hold > 0 {
		time.Sleep(*hold)
	}
	fmt.Printf("Hovered: %s\n", selector)
	return nil
}

func cmdDrag(args []string) error {
	fs := newFlagSet("drag", "usage: cdp drag <name> \".from\" \".to\"")
	fromIndex := fs.Int("from-index", 0, "Index within the source selector (0-based)")
	toIndex := fs.Int("to-index", 0, "Index within the target selector (0-based)")
	delay := fs.Duration("delay", 0, "Delay between drag events (e.g. 50ms)")
	timeout := fs.Duration("timeout", 8*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp drag <name> \".from\" \".to\"")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 3 {
		return errors.New("usage: cdp drag <name> \".from\" \".to\"")
	}
	name := pos[0]
	fromSelector := pos[1]
	toSelector := pos[2]
	if len(pos) > 3 {
		return fmt.Errorf("unexpected argument: %s", pos[3])
	}
	if err := rejectUnsupportedSelector(fromSelector, "drag --from", false); err != nil {
		return err
	}
	if err := rejectUnsupportedSelector(toSelector, "drag --to", false); err != nil {
		return err
	}
	if *fromIndex < 0 || *toIndex < 0 {
		return errors.New("indices must be >= 0")
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

	delayMS := delay.Milliseconds()
	expression := fmt.Sprintf(`(async () => {
        const fromSelector = %s;
        const toSelector = %s;
        const fromIndex = %d;
        const toIndex = %d;
        const delayMs = %d;

        function sleep(ms) {
            if (!ms || ms <= 0) return Promise.resolve();
            return new Promise(resolve => setTimeout(resolve, ms));
        }

        function pick(selector, index) {
            const list = Array.from(document.querySelectorAll(selector));
            if (!list.length) return {el: null, list};
            const idx = Math.min(Math.max(index, 0), list.length - 1);
            return {el: list[idx], list};
        }

        const fromPick = pick(fromSelector, fromIndex);
        const toPick = pick(toSelector, toIndex);
        if (!fromPick.el) throw new Error("no element matched selector: " + fromSelector);
        if (!toPick.el) throw new Error("no element matched selector: " + toSelector);

        const fromEl = fromPick.el;
        const toEl = toPick.el;

        const fromRect = fromEl.getBoundingClientRect();
        const toRect = toEl.getBoundingClientRect();
        const fromPt = {x: fromRect.left + Math.max(2, Math.min(fromRect.width - 2, fromRect.width / 2)),
                        y: fromRect.top + Math.max(2, Math.min(fromRect.height - 2, fromRect.height / 2))};
        const toPt = {x: toRect.left + Math.max(2, Math.min(toRect.width - 2, toRect.width / 2)),
                      y: toRect.top + Math.max(2, Math.min(toRect.height - 2, toRect.height / 2))};

        let dataTransfer = null;
        if (typeof DataTransfer !== "undefined") {
            dataTransfer = new DataTransfer();
        } else {
            dataTransfer = {
                data: {},
                setData(type, val) { this.data[type] = String(val); },
                getData(type) { return this.data[type] || ""; },
                clearData(type) { if (type) delete this.data[type]; else this.data = {}; },
                effectAllowed: "all",
                dropEffect: "move",
                types: [],
            };
        }

        function dispatchMouse(el, type, pt) {
            const evt = new MouseEvent(type, {
                bubbles: true,
                cancelable: true,
                clientX: pt.x,
                clientY: pt.y,
                button: 0,
                buttons: type === "mouseup" ? 0 : 1,
            });
            el.dispatchEvent(evt);
        }

        function dispatchDrag(el, type, pt) {
            const evt = new DragEvent(type, {
                bubbles: true,
                cancelable: true,
                clientX: pt.x,
                clientY: pt.y,
                dataTransfer,
            });
            el.dispatchEvent(evt);
        }

        if (fromEl.scrollIntoView) fromEl.scrollIntoView({block: "center", inline: "center"});
        if (toEl.scrollIntoView) toEl.scrollIntoView({block: "center", inline: "center"});

        dispatchMouse(fromEl, "mousedown", fromPt);
        dispatchDrag(fromEl, "dragstart", fromPt);
        await sleep(delayMs);
        dispatchDrag(toEl, "dragenter", toPt);
        dispatchDrag(toEl, "dragover", toPt);
        await sleep(delayMs);
        dispatchDrag(toEl, "drop", toPt);
        dispatchDrag(fromEl, "dragend", toPt);
        dispatchMouse(toEl, "mouseup", toPt);
        return {fromIndex, toIndex, fromCount: fromPick.list.length, toCount: toPick.list.length};
    })()`, strconv.Quote(fromSelector), strconv.Quote(toSelector), *fromIndex, *toIndex, delayMS)

	if _, err := handle.client.Evaluate(ctx, expression); err != nil {
		return err
	}
	fmt.Printf("Dragged: %s[%d] -> %s[%d]\n", fromSelector, *fromIndex, toSelector, *toIndex)
	return nil
}

func cmdGesture(args []string) error {
	usage := "usage: cdp gesture <name> \".selector\" \"x1,y1 x2,y2 ...\"  (draw, swipe, slide, trace)"
	fs := newFlagSet("gesture", usage+"\n\nPress-move-release along a path within an element.\nCoordinates are relative (0-1) to the element's bounding box.\n\nExamples:\n  cdp gesture mgr \"canvas\" \"0.1,0.5 0.9,0.5\"        # horizontal stroke\n  cdp gesture mgr \".slider\" \"0.0,0.5 1.0,0.5\"        # slide fully right\n  cdp gesture mgr \".pad\" \"0.2,0.2 0.8,0.2 0.8,0.8\"   # L-shaped path")
	delay := fs.Duration("delay", 50*time.Millisecond, "Delay between pointer events")
	timeout := fs.Duration("timeout", 12*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New(usage)
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 3 {
		return errors.New(usage)
	}
	name := pos[0]
	selector := pos[1]
	pathStr := pos[2]
	if len(pos) > 3 {
		return fmt.Errorf("unexpected argument: %s", pos[3])
	}
	if err := rejectUnsupportedSelector(selector, "gesture", false); err != nil {
		return err
	}

	// Parse path: "x1,y1 x2,y2 ..." where each coord is 0-1 relative to element bounds.
	parts := strings.Fields(pathStr)
	if len(parts) < 2 {
		return errors.New("path must have at least 2 points (e.g. \"0.1,0.5 0.9,0.5\")")
	}
	type point struct{ x, y float64 }
	points := make([]point, 0, len(parts))
	for _, p := range parts {
		xy := strings.SplitN(p, ",", 2)
		if len(xy) != 2 {
			return fmt.Errorf("invalid point %q (expected x,y)", p)
		}
		x, errX := strconv.ParseFloat(xy[0], 64)
		y, errY := strconv.ParseFloat(xy[1], 64)
		if errX != nil || errY != nil {
			return fmt.Errorf("invalid point %q (expected numeric x,y)", p)
		}
		points = append(points, point{x, y})
	}

	// Serialize points as JSON array for the JS expression.
	var pointsJSON strings.Builder
	pointsJSON.WriteByte('[')
	for i, pt := range points {
		if i > 0 {
			pointsJSON.WriteByte(',')
		}
		fmt.Fprintf(&pointsJSON, "[%g,%g]", pt.x, pt.y)
	}
	pointsJSON.WriteByte(']')

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

	delayMS := delay.Milliseconds()
	expression := fmt.Sprintf(`(async () => {
        const selector = %s;
        const points = %s;
        const delayMs = %d;

        function sleep(ms) {
            if (!ms || ms <= 0) return Promise.resolve();
            return new Promise(resolve => setTimeout(resolve, ms));
        }

        const el = document.querySelector(selector);
        if (!el) throw new Error("no element matched selector: " + selector);

        if (el.scrollIntoView) {
            el.scrollIntoView({block: "center", inline: "center"});
        }
        if (el.focus) {
            el.focus();
        }

        const rect = el.getBoundingClientRect();

        function toAbs(rx, ry) {
            return { x: rect.left + rx * rect.width, y: rect.top + ry * rect.height };
        }

        function dispatchPointer(type, x, y, isDown) {
            if (typeof PointerEvent !== "undefined") {
                el.dispatchEvent(new PointerEvent(type, {
                    bubbles: true,
                    cancelable: true,
                    clientX: x,
                    clientY: y,
                    pointerType: "mouse",
                    button: 0,
                    buttons: isDown ? 1 : 0,
                }));
            }
        }

        function dispatchMouse(type, x, y, isDown) {
            el.dispatchEvent(new MouseEvent(type, {
                bubbles: true,
                cancelable: true,
                clientX: x,
                clientY: y,
                button: 0,
                buttons: isDown ? 1 : 0,
            }));
        }

        const first = toAbs(points[0][0], points[0][1]);
        dispatchPointer("pointerdown", first.x, first.y, true);
        dispatchMouse("mousedown", first.x, first.y, true);

        for (let i = 1; i < points.length; i++) {
            await sleep(delayMs);
            const pt = toAbs(points[i][0], points[i][1]);
            dispatchPointer("pointermove", pt.x, pt.y, true);
            dispatchMouse("mousemove", pt.x, pt.y, true);
        }

        const last = toAbs(points[points.length - 1][0], points[points.length - 1][1]);
        await sleep(delayMs);
        dispatchPointer("pointerup", last.x, last.y, false);
        dispatchMouse("mouseup", last.x, last.y, false);

        return { points: points.length };
    })()`, strconv.Quote(selector), pointsJSON.String(), delayMS)

	if _, err := handle.client.Evaluate(ctx, expression); err != nil {
		return err
	}
	fmt.Printf("Gesture (%d points) on: %s\n", len(points), selector)
	return nil
}

func cmdKey(args []string) error {
	usage := "usage: cdp key <name> KEYS [--element \".selector\"] [--cdp]"
	fs := newFlagSet("key", usage+"\n\nSend a key press. KEYS is key names joined by + for combos.\n\nExamples:\n  cdp key mgr Enter\n  cdp key mgr Ctrl+c\n  cdp key mgr Ctrl+Shift+s\n  cdp key mgr ArrowDown\n\nKey names: Enter, Escape, Tab, Backspace, Delete, Space, ArrowUp/Down/Left/Right, Home, End, PageUp, PageDown, F1-F12, Ctrl, Shift, Alt, Meta, or any character.")
	element := fs.String("element", "", "Focus this element before sending the key")
	useCDP := fs.Bool("cdp", false, "Use CDP Input.dispatchKeyEvent instead of JS KeyboardEvent")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New(usage)
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New(usage)
	}
	name := pos[0]
	spec := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	if *element != "" {
		if err := rejectUnsupportedSelector(*element, "key --element", false); err != nil {
			return err
		}
	}

	keySpec, err := parseKeySpec(spec)
	if err != nil {
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

	if *element != "" {
		expression := fmt.Sprintf(`(() => {
            const el = document.querySelector(%s);
            if (!el) { throw new Error("no element matched selector: " + %s); }
            if (el.scrollIntoView) { el.scrollIntoView({block: "center", inline: "center"}); }
            if (el.focus) { el.focus(); }
            return true;
        })()`, strconv.Quote(*element), strconv.Quote(*element))
		if _, err := handle.client.Evaluate(ctx, expression); err != nil {
			return err
		}
	}

	if !*useCDP {
		expression := fmt.Sprintf(`(() => {
            const opts = {
              key: %s,
              code: %s,
              bubbles: true,
              ctrlKey: %t,
              shiftKey: %t,
              altKey: %t,
              metaKey: %t
            };
            document.dispatchEvent(new KeyboardEvent('keydown', opts));
            document.dispatchEvent(new KeyboardEvent('keyup', opts));
            return true;
        })()`, strconv.Quote(keySpec.key), strconv.Quote(keySpec.code),
			keySpec.modifiers&2 != 0, keySpec.modifiers&8 != 0, keySpec.modifiers&1 != 0, keySpec.modifiers&4 != 0)
		if _, err := handle.client.Evaluate(ctx, expression); err != nil {
			return err
		}
		fmt.Printf("Key (js): %s\n", spec)
		return nil
	}

	downType := "keyDown"
	if keySpec.modifiers != 0 || keySpec.text == "" {
		downType = "rawKeyDown"
	}
	downParams := keyDispatchParams(downType, keySpec)
	upParams := keyDispatchParams("keyUp", keySpec)
	// Ensure the page is in a valid state to receive input events.
	if err := handle.client.Call(ctx, "Page.bringToFront", map[string]interface{}{}, nil); err != nil {
		return err
	}
	if handle.session.TargetID != "" {
		if err := handle.client.Call(ctx, "Target.activateTarget", map[string]interface{}{
			"targetId": handle.session.TargetID,
		}, nil); err != nil {
			return err
		}
	}

	if err := handle.client.Call(ctx, "Input.dispatchKeyEvent", downParams, nil); err != nil {
		return err
	}
	if err := handle.client.Call(ctx, "Input.dispatchKeyEvent", upParams, nil); err != nil {
		return err
	}

	fmt.Printf("Key: %s\n", spec)
	return nil
}

func cmdType(args []string) error {
	fs := newFlagSet("type", "usage: cdp type <name> \".selector\" \"text\" [--has-text REGEX] [--att-value REGEX]\n(also supports inline :has-text(...) at the end of the selector)")
	appendText := fs.Bool("append", false, "Append text instead of replacing")
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp type <name> \".selector\" \"text\" [--has-text REGEX] [--att-value REGEX]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 3 {
		return errors.New("usage: cdp type <name> \".selector\" \"text\" [--has-text REGEX] [--att-value REGEX]")
	}
	name := pos[0]
	selector := pos[1]
	text := pos[2]
	if len(pos) > 3 {
		return fmt.Errorf("unexpected argument: %s", pos[3])
	}
	selector, inlineHasText, hasInline, err := parseInlineHasText(selector)
	if err != nil {
		return err
	}
	if err := rejectUnsupportedSelector(selector, "type", true); err != nil {
		return err
	}
	selector = autoQuoteAttrValues(selector)
	hasTextValue := *hasText
	if hasInline {
		hasTextValue = inlineHasText
	}
	hasTextValue = escapeLeadingPlusRegexSpec(hasTextValue)
	attValueValue := escapeLeadingPlusRegexSpec(*attValue)

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
        const selector = %s;
        const hasTextSpec = %s;
        const attValueSpec = %s;
        const inputText = %s;

        function compileRegex(spec) {
            if (!spec) return null;
            if (spec.length >= 2 && spec[0] === "/") {
                const last = spec.lastIndexOf("/");
                if (last > 0) {
                    const pattern = spec.slice(1, last);
                    const flags = spec.slice(last + 1);
                    try { return new RegExp(pattern, flags); } catch (e) {}
                }
            }
            try { return new RegExp(spec); } catch (e) {
                throw new Error("invalid regex: " + spec + " (" + (e && e.message ? e.message : e) + ")");
            }
        }

        const hasTextRe = compileRegex(hasTextSpec);
        const attValueRe = compileRegex(attValueSpec);

        function elementText(el) {
            return (el && (el.innerText || el.textContent)) || "";
        }

        function attrValueMatches(el, re) {
            const attrs = (el && el.attributes) || null;
            if (!attrs) return false;
            for (let i = 0; i < attrs.length; i++) {
                const v = attrs[i] && attrs[i].value ? attrs[i].value : "";
                if (re.test(v)) return true;
            }
            return false;
        }

        const list = Array.from(document.querySelectorAll(selector));
        let el = null;
        for (let i = 0; i < list.length; i++) {
            const cand = list[i];
            if (hasTextRe && !hasTextRe.test(elementText(cand))) continue;
            if (attValueRe && !attrValueMatches(cand, attValueRe)) continue;
            el = cand;
            break;
        }
        if (!el) {
            throw new Error("no element matched selector/filters: " + selector);
        }
        const append = %t;
        if (el.scrollIntoView) {
            el.scrollIntoView({block: "center", inline: "center"});
        }
        if (el.focus) {
            el.focus();
        }
        const tag = el.tagName ? el.tagName.toLowerCase() : "";
        const isTextInput = tag === "input" || tag === "textarea";
        if (isTextInput) {
            const inputType = (el.getAttribute && el.getAttribute("type") ? el.getAttribute("type") : el.type) || "";
            const normalizedType = inputType ? String(inputType).toLowerCase() : "";
            const useNativeValue = tag === "input" && normalizedType === "number";
            if (useNativeValue) {
                const next = append ? String(el.value || "") + String(inputText) : String(inputText);
                const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
                if (setter) {
                    setter.call(el, next);
                } else {
                    el.value = next;
                }
                try {
                    el.dispatchEvent(new Event("input", {bubbles: true}));
                    el.dispatchEvent(new Event("change", {bubbles: true}));
                } catch (e) {}
                return { found: true, editable: true, contentEditable: false, handled: true };
            }
            if (!append) {
                el.value = "";
            }
            if (el.setSelectionRange) {
                try {
                    const end = el.value.length;
                    el.setSelectionRange(end, end);
                } catch (e) {}
            }
            return { found: true, editable: true, contentEditable: false, handled: false };
        }
        if (el.isContentEditable) {
            if (!append) {
                el.textContent = "";
            }
            const range = document.createRange();
            range.selectNodeContents(el);
            range.collapse(false);
            const sel = window.getSelection();
            sel.removeAllRanges();
            sel.addRange(range);
            return { found: true, editable: true, contentEditable: true, handled: false };
        }
        return { found: true, editable: false, contentEditable: false, handled: false };
    })()`, strconv.Quote(selector), strconv.Quote(hasTextValue), strconv.Quote(attValueValue), strconv.Quote(text), *appendText)

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	state, ok := value.(map[string]interface{})
	if !ok || state["found"] != true {
		return errors.New("selector not found")
	}
	if handled, _ := state["handled"].(bool); handled {
		fmt.Printf("Typed into: %s\n", selector)
		return nil
	}
	editable, _ := state["editable"].(bool)
	if editable {
		if err := handle.client.Call(ctx, "Input.insertText", map[string]interface{}{
			"text": text,
		}, nil); err != nil {
			return err
		}
		fmt.Printf("Typed into: %s\n", selector)
		return nil
	}

	fallback := fmt.Sprintf(`(() => {
        const selector = %s;
        const hasTextSpec = %s;
        const attValueSpec = %s;

        function compileRegex(spec) {
            if (!spec) return null;
            if (spec.length >= 2 && spec[0] === "/") {
                const last = spec.lastIndexOf("/");
                if (last > 0) {
                    const pattern = spec.slice(1, last);
                    const flags = spec.slice(last + 1);
                    try { return new RegExp(pattern, flags); } catch (e) {}
                }
            }
            try { return new RegExp(spec); } catch (e) {
                return null;
            }
        }

        const hasTextRe = compileRegex(hasTextSpec);
        const attValueRe = compileRegex(attValueSpec);

        function elementText(el) {
            return (el && (el.innerText || el.textContent)) || "";
        }

        function attrValueMatches(el, re) {
            const attrs = (el && el.attributes) || null;
            if (!attrs) return false;
            for (let i = 0; i < attrs.length; i++) {
                const v = attrs[i] && attrs[i].value ? attrs[i].value : "";
                if (re.test(v)) return true;
            }
            return false;
        }

        const list = Array.from(document.querySelectorAll(selector));
        let el = null;
        for (let i = 0; i < list.length; i++) {
            const cand = list[i];
            if (hasTextRe && !hasTextRe.test(elementText(cand))) continue;
            if (attValueRe && !attrValueMatches(cand, attValueRe)) continue;
            el = cand;
            break;
        }
        if (!el) { return false; }
        const append = %t;
        if (!append) {
            el.textContent = "";
        }
        el.textContent = append ? el.textContent + %s : %s;
        return true;
    })()`, strconv.Quote(selector), strconv.Quote(hasTextValue), strconv.Quote(attValueValue), *appendText, strconv.Quote(text), strconv.Quote(text))
	if _, err := handle.client.Evaluate(ctx, fallback); err != nil {
		return err
	}
	fmt.Printf("Typed into: %s\n", selector)
	return nil
}

func cmdScroll(args []string) error {
	fs := newFlagSet("scroll", "usage: cdp scroll <name> <yPx> [--x <xPx>] [--element \".selector\"] [--emit]")
	scrollX := fs.Float64("x", 0, "Horizontal scroll delta in pixels (can be negative)")
	element := fs.String("element", "", "Scroll inside an element matched by selector")
	emit := fs.Bool("emit", true, "Dispatch scroll events after scrolling")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp scroll <name> <yPx> [--x <xPx>] [--element \".selector\"] [--emit]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp scroll <name> <yPx> [--x <xPx>] [--element \".selector\"] [--emit]")
	}
	name := pos[0]
	yStr := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	if *element != "" {
		if err := rejectUnsupportedSelector(*element, "scroll --element", false); err != nil {
			return err
		}
	}

	scrollY, err := strconv.ParseFloat(yStr, 64)
	if err != nil {
		return fmt.Errorf("invalid yPx %q: %w", yStr, err)
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

	yJS := strconv.FormatFloat(scrollY, 'f', -1, 64)
	xJS := strconv.FormatFloat(*scrollX, 'f', -1, 64)
	expression := fmt.Sprintf(`(() => {
        const SCROLL_Y_PX = %s; // can be positive (down) or negative (up)
        const SCROLL_X_PX = %s; // can be positive (right) or negative (left)
        const ELEMENT_SELECTOR = %s;
        const EMIT = %t;

        const el = ELEMENT_SELECTOR ? document.querySelector(ELEMENT_SELECTOR) : (document.scrollingElement || document.documentElement);
        if (ELEMENT_SELECTOR && !el) {
            throw new Error("no element matched selector: " + ELEMENT_SELECTOR);
        }

        if (ELEMENT_SELECTOR) {
            try {
                el.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "instant" });
            } catch (e) {
                try {
                    el.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "auto" });
                } catch (e2) {
                    el.scrollTop = (el.scrollTop || 0) + SCROLL_Y_PX;
                    el.scrollLeft = (el.scrollLeft || 0) + SCROLL_X_PX;
                }
            }
        } else {
            // Scroll in a way that triggers normal scroll handling.
            try {
                window.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "instant" });
            } catch (e) {
                // "instant" isn't standard; fall back for older engines.
                window.scrollBy({ top: SCROLL_Y_PX, left: SCROLL_X_PX, behavior: "auto" });
            }
        }

        if (EMIT) {
            const evt = new Event("scroll", { bubbles: true });
            if (ELEMENT_SELECTOR) {
                el.dispatchEvent(evt);
            } else {
                window.dispatchEvent(evt);
                document.dispatchEvent(evt);
            }
        }

        // Return the new scroll position for sanity-checking.
        return { scrollTop: el.scrollTop, scrollLeft: el.scrollLeft };
    })()`, yJS, xJS, strconv.Quote(*element), *emit)

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	posMap, ok := value.(map[string]interface{})
	if !ok {
		fmt.Printf("Scrolled by y=%s x=%s\n", yJS, xJS)
		return nil
	}
	fmt.Printf("Scrolled by y=%s x=%s -> scrollTop=%s scrollLeft=%s\n", yJS, xJS, formatScrollNumber(posMap["scrollTop"]), formatScrollNumber(posMap["scrollLeft"]))
	return nil
}

func cmdUpload(args []string) error {
	fs := newFlagSet("upload", "usage: cdp upload <name> \"input[type=file]\" <file1> [file2 ...] [--wait]")
	waitFlag := fs.Bool("wait", false, "Wait for the selector to exist before uploading")
	poll := fs.Duration("poll", 200*time.Millisecond, "Polling interval when using --wait")
	timeout := fs.Duration("timeout", 10*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp upload <name> \"input[type=file]\" <file1> [file2 ...] [--wait]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 3 {
		return errors.New("usage: cdp upload <name> \"input[type=file]\" <file1> [file2 ...] [--wait]")
	}
	name := pos[0]
	selector := pos[1]
	filesRaw := pos[2:]
	if err := rejectUnsupportedSelector(selector, "upload", false); err != nil {
		return err
	}

	files := make([]string, 0, len(filesRaw))
	for _, f := range filesRaw {
		p, err := expandPath(f)
		if err != nil {
			return err
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("not a file: %s", abs)
		}
		files = append(files, abs)
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

	if *waitFlag {
		if err := waitForSelector(ctx, handle.client, selector, *poll); err != nil {
			return err
		}
	}

	if err := handle.client.Call(ctx, "DOM.enable", nil, nil); err != nil {
		return err
	}
	nodeID, err := resolveNodeID(ctx, handle.client, selector)
	if err != nil {
		return err
	}
	if nodeID == 0 {
		return fmt.Errorf("no element matched selector: %s", selector)
	}
	if err := handle.client.Call(ctx, "DOM.setFileInputFiles", map[string]interface{}{
		"nodeId": nodeID,
		"files":  files,
	}, nil); err != nil {
		return err
	}

	// Nudge frameworks that listen to change events only.
	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) return false;
        try {
            el.dispatchEvent(new Event("input", {bubbles: true}));
            el.dispatchEvent(new Event("change", {bubbles: true}));
        } catch (e) {}
        return true;
    })()`, strconv.Quote(selector))
	if _, err := handle.client.Evaluate(ctx, expression); err != nil {
		return err
	}

	fmt.Printf("Uploaded %d file(s) into %s\n", len(files), selector)
	return nil
}

func cmdDOM(args []string) error {
	fs := newFlagSet("dom", "usage: cdp dom <name> \".selector\"")
	pretty := fs.Bool("pretty", true, "Pretty print output")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	switch len(args) {
	case 0:
		fs.Usage()
		return errors.New("usage: cdp dom <name> \".selector\"")
	case 1:
		if isHelpArg(args[0]) {
			fs.Usage()
			return nil
		}
		return errors.New("usage: cdp dom <name> \".selector\"")
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp dom <name> \".selector\"")
	}
	name := pos[0]
	selector := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
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
	fs := newFlagSet("styles", "usage: cdp styles <name> \".selector\"")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	switch len(args) {
	case 0:
		fs.Usage()
		return errors.New("usage: cdp styles <name> \".selector\"")
	case 1:
		if isHelpArg(args[0]) {
			fs.Usage()
			return nil
		}
		return errors.New("usage: cdp styles <name> \".selector\"")
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp styles <name> \".selector\"")
	}
	name := pos[0]
	selector := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
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
	fs := newFlagSet("rect", "usage: cdp rect <name> \".selector\"")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	switch len(args) {
	case 0:
		fs.Usage()
		return errors.New("usage: cdp rect <name> \".selector\"")
	case 1:
		if isHelpArg(args[0]) {
			fs.Usage()
			return nil
		}
		return errors.New("usage: cdp rect <name> \".selector\"")
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp rect <name> \".selector\"")
	}
	name := pos[0]
	selector := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
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
	fs := newFlagSet("screenshot", "usage: cdp screenshot <name> [--selector ...]")
	selector := fs.String("selector", "", "CSS selector to crop")
	output := fs.String("output", "screenshot.png", "Output file path")
	fullPage := fs.Bool("full-page", false, "Capture beyond the current viewport (may cause resize/reflow in headful Chrome)")
	cdpClip := fs.Bool("cdp-clip", false, "When using --selector, crop via CDP clip (may resize/reflow); default is capture viewport then crop locally")
	scrollIntoView := fs.Bool("scroll-into-view", true, "When using --selector (without --cdp-clip), scroll the element into view before capture")
	timeout := fs.Duration("timeout", 15*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp screenshot <name> [--selector ...]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return errors.New("usage: cdp screenshot <name> [--selector ...]")
	}
	name := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
	}
	if *selector != "" {
		if err := rejectUnsupportedSelector(*selector, "screenshot --selector", false); err != nil {
			return err
		}
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
		"format":      "png",
		"fromSurface": true,
	}

	// Default to "viewport only" to avoid the headful flicker/resize path in Chromium.
	// `captureBeyondViewport=true` is still available via --full-page (or --cdp-clip).
	params["captureBeyondViewport"] = *fullPage

	var crop *screenshotCrop
	if *selector != "" {
		if *cdpClip {
			clip, err := resolveClip(ctx, handle.client, *selector)
			if err != nil {
				return err
			}
			if clip == nil {
				return fmt.Errorf("selector %s not found", *selector)
			}
			params["clip"] = clip
			params["captureBeyondViewport"] = true
		} else {
			// Compute a viewport-relative crop rect, then crop locally to avoid Chromium resizing the view.
			if *scrollIntoView {
				if err := handle.client.Call(ctx, "DOM.enable", nil, nil); err != nil {
					return err
				}
				nodeID, err := resolveNodeID(ctx, handle.client, *selector)
				if err != nil {
					return err
				}
				if nodeID == 0 {
					return fmt.Errorf("selector %s not found", *selector)
				}
				_ = handle.client.Call(ctx, "DOM.scrollIntoViewIfNeeded", map[string]interface{}{"nodeId": nodeID}, nil)
			}
			var err error
			crop, err = resolveViewportCrop(ctx, handle.client, *selector)
			if err != nil {
				return err
			}
			if crop == nil {
				return fmt.Errorf("selector %s not found", *selector)
			}
		}
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

	if crop != nil {
		cropped, err := cropPNG(data, *crop)
		if err != nil {
			return err
		}
		data = cropped
	}

	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("Saved %s (%d bytes)\n", *output, len(data))
	return nil
}

type screenshotCrop struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
	DPR    float64
}

func resolveViewportCrop(ctx context.Context, client *cdp.Client, selector string) (*screenshotCrop, error) {
	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) return null;
        const r = el.getBoundingClientRect();
        const dpr = (typeof window !== "undefined" && window.devicePixelRatio) ? window.devicePixelRatio : 1;
        return {x: r.left, y: r.top, width: r.width, height: r.height, dpr};
    })()`, strconv.Quote(selector))

	value, err := client.Evaluate(ctx, expression)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	raw, ok := value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected crop result type %T", value)
	}

	getNum := func(key string) (float64, error) {
		v, ok := raw[key]
		if !ok {
			return 0, fmt.Errorf("missing %q in crop result", key)
		}
		n, ok := v.(float64)
		if !ok {
			return 0, fmt.Errorf("unexpected %q type %T", key, v)
		}
		return n, nil
	}

	x, err := getNum("x")
	if err != nil {
		return nil, err
	}
	y, err := getNum("y")
	if err != nil {
		return nil, err
	}
	w, err := getNum("width")
	if err != nil {
		return nil, err
	}
	h, err := getNum("height")
	if err != nil {
		return nil, err
	}
	dpr, err := getNum("dpr")
	if err != nil {
		return nil, err
	}
	if dpr <= 0 {
		dpr = 1
	}
	return &screenshotCrop{X: x, Y: y, Width: w, Height: h, DPR: dpr}, nil
}

func cropPNG(pngBytes []byte, crop screenshotCrop) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()

	// Convert CSS px (viewport) -> image px. Use floor/ceil to avoid off-by-one clipping.
	left := int(math.Floor(crop.X * crop.DPR))
	top := int(math.Floor(crop.Y * crop.DPR))
	right := int(math.Ceil((crop.X + crop.Width) * crop.DPR))
	bottom := int(math.Ceil((crop.Y + crop.Height) * crop.DPR))

	// Clamp to image bounds.
	if left < b.Min.X {
		left = b.Min.X
	}
	if top < b.Min.Y {
		top = b.Min.Y
	}
	if right > b.Max.X {
		right = b.Max.X
	}
	if bottom > b.Max.Y {
		bottom = b.Max.Y
	}
	if right <= left || bottom <= top {
		return nil, fmt.Errorf("crop rectangle is empty after clamping (x=%d y=%d w=%d h=%d)", left, top, right-left, bottom-top)
	}

	r := image.Rect(left, top, right, bottom)
	var sub image.Image
	if si, ok := img.(interface {
		SubImage(r image.Rectangle) image.Image
	}); ok {
		sub = si.SubImage(r)
	} else {
		// Fallback: copy pixels into a new RGBA buffer.
		dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		for y := 0; y < r.Dy(); y++ {
			for x := 0; x < r.Dx(); x++ {
				dst.Set(x, y, img.At(r.Min.X+x, r.Min.Y+y))
			}
		}
		sub = dst
	}

	var out bytes.Buffer
	if err := png.Encode(&out, sub); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
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

	// DOM box model coordinates are viewport-relative. `Page.captureScreenshot` clip expects
	// document/page coordinates, so we offset by the visual viewport scroll position.
	var metrics struct {
		VisualViewport struct {
			PageX float64 `json:"pageX"`
			PageY float64 `json:"pageY"`
		} `json:"visualViewport"`
	}
	if err := client.Call(ctx, "Page.getLayoutMetrics", nil, &metrics); err != nil {
		return nil, err
	}

	clip := map[string]interface{}{
		"x":      box.Model.Content[0] + metrics.VisualViewport.PageX,
		"y":      box.Model.Content[1] + metrics.VisualViewport.PageY,
		"width":  box.Model.Width,
		"height": box.Model.Height,
		"scale":  1,
	}
	return clip, nil
}

func resolveNodeID(ctx context.Context, client *cdp.Client, selector string) (int, error) {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := client.Call(ctx, "DOM.getDocument", map[string]interface{}{"depth": -1, "pierce": true}, &doc); err != nil {
		return 0, err
	}
	var node struct {
		NodeID int `json:"nodeId"`
	}
	if err := client.Call(ctx, "DOM.querySelector", map[string]interface{}{
		"nodeId":   doc.Root.NodeID,
		"selector": selector,
	}, &node); err != nil {
		return 0, err
	}
	return node.NodeID, nil
}

func formatScrollNumber(value interface{}) string {
	n, ok := value.(float64)
	if !ok {
		return fmt.Sprint(value)
	}
	rounded := math.Round(n*100) / 100
	s := strconv.FormatFloat(rounded, 'f', 2, 64)
	s = strings.TrimSuffix(s, "00")
	s = strings.TrimSuffix(s, "0")
	if strings.HasSuffix(s, ".") {
		s = strings.TrimSuffix(s, ".")
	}
	return s
}

type keySpec struct {
	key       string
	code      string
	text      string
	keyCode   int
	modifiers int
}

func keyDispatchParams(eventType string, spec keySpec) map[string]interface{} {
	params := map[string]interface{}{
		"type":      eventType,
		"modifiers": spec.modifiers,
		"key":       spec.key,
	}
	if spec.code != "" {
		params["code"] = spec.code
	}
	if spec.keyCode > 0 {
		params["windowsVirtualKeyCode"] = spec.keyCode
		params["nativeVirtualKeyCode"] = spec.keyCode
	}
	// Only include text for unmodified keyDown events.
	if eventType == "keyDown" && spec.text != "" && spec.modifiers == 0 {
		params["text"] = spec.text
		params["unmodifiedText"] = spec.text
	}
	return params
}

func parseKeySpec(spec string) (keySpec, error) {
	if strings.TrimSpace(spec) == "" {
		return keySpec{}, errors.New("keys spec cannot be empty")
	}

	const (
		modAlt   = 1
		modCtrl  = 2
		modMeta  = 4
		modShift = 8
	)

	modifierMap := map[string]int{
		"alt":     modAlt,
		"ctrl":    modCtrl,
		"control": modCtrl,
		"meta":    modMeta,
		"cmd":     modMeta,
		"command": modMeta,
		"win":     modMeta,
		"windows": modMeta,
		"shift":   modShift,
	}

	namedKeys := map[string]keySpec{
		"enter":      {key: "Enter", code: "Enter", keyCode: 13},
		"escape":     {key: "Escape", code: "Escape", keyCode: 27},
		"esc":        {key: "Escape", code: "Escape", keyCode: 27},
		"tab":        {key: "Tab", code: "Tab", keyCode: 9},
		"backspace":  {key: "Backspace", code: "Backspace", keyCode: 8},
		"delete":     {key: "Delete", code: "Delete", keyCode: 46},
		"del":        {key: "Delete", code: "Delete", keyCode: 46},
		"space":      {key: " ", code: "Space", keyCode: 32, text: " "},
		"spacebar":   {key: " ", code: "Space", keyCode: 32, text: " "},
		"arrowup":    {key: "ArrowUp", code: "ArrowUp", keyCode: 38},
		"arrowdown":  {key: "ArrowDown", code: "ArrowDown", keyCode: 40},
		"arrowleft":  {key: "ArrowLeft", code: "ArrowLeft", keyCode: 37},
		"arrowright": {key: "ArrowRight", code: "ArrowRight", keyCode: 39},
		"home":       {key: "Home", code: "Home", keyCode: 36},
		"end":        {key: "End", code: "End", keyCode: 35},
		"pageup":     {key: "PageUp", code: "PageUp", keyCode: 33},
		"pagedown":   {key: "PageDown", code: "PageDown", keyCode: 34},
		"pgup":       {key: "PageUp", code: "PageUp", keyCode: 33},
		"pgdn":       {key: "PageDown", code: "PageDown", keyCode: 34},
	}

	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("f%d", i)
		namedKeys[name] = keySpec{key: strings.ToUpper(name), code: strings.ToUpper(name), keyCode: 111 + i}
	}

	tokens := strings.Split(spec, "+")
	modifiers := 0
	var modifierTokens []string
	keyToken := ""
	for _, t := range tokens {
		s := strings.TrimSpace(t)
		if s == "" {
			return keySpec{}, errors.New("invalid empty key token")
		}
		lower := strings.ToLower(s)
		if mod, ok := modifierMap[lower]; ok {
			modifiers |= mod
			modifierTokens = append(modifierTokens, lower)
			continue
		}
		if keyToken != "" {
			return keySpec{}, errors.New("only one non-modifier key is supported")
		}
		keyToken = s
	}

	if keyToken == "" {
		if len(modifierTokens) != 1 {
			return keySpec{}, errors.New("spec must include a non-modifier key (or a single modifier)")
		}
		keyToken = modifierTokens[0]
	}

	lowerKey := strings.ToLower(keyToken)
	if mod, ok := modifierMap[lowerKey]; ok {
		modifiers |= mod
		switch mod {
		case modCtrl:
			return keySpec{key: "Control", code: "ControlLeft", keyCode: 17, modifiers: modifiers}, nil
		case modShift:
			return keySpec{key: "Shift", code: "ShiftLeft", keyCode: 16, modifiers: modifiers}, nil
		case modAlt:
			return keySpec{key: "Alt", code: "AltLeft", keyCode: 18, modifiers: modifiers}, nil
		case modMeta:
			return keySpec{key: "Meta", code: "MetaLeft", keyCode: 91, modifiers: modifiers}, nil
		}
	}

	if named, ok := namedKeys[lowerKey]; ok {
		named.modifiers = modifiers
		if named.text != "" && (modifiers&(modCtrl|modAlt|modMeta)) != 0 {
			named.text = ""
		}
		return named, nil
	}

	runes := []rune(keyToken)
	if len(runes) != 1 {
		return keySpec{}, fmt.Errorf("unknown key %q", keyToken)
	}
	r := runes[0]
	if unicode.IsLetter(r) && unicode.IsUpper(r) && (modifiers&modShift) == 0 {
		modifiers |= modShift
	}
	key := string(r)
	code := ""
	keyCode := 0

	upper := unicode.ToUpper(r)
	if upper >= 'A' && upper <= 'Z' {
		code = fmt.Sprintf("Key%c", upper)
		keyCode = int(upper)
		if (modifiers & modShift) != 0 {
			key = string(upper)
		} else {
			key = strings.ToLower(string(upper))
		}
	} else if r >= '0' && r <= '9' {
		code = fmt.Sprintf("Digit%c", r)
		keyCode = int(r)
	} else if r <= 0x7f {
		keyCode = int(r)
	}

	text := key
	if (modifiers & (modCtrl | modAlt | modMeta)) != 0 {
		text = ""
	}

	return keySpec{
		key:       key,
		code:      code,
		text:      text,
		keyCode:   keyCode,
		modifiers: modifiers,
	}, nil
}

func parseInlineHasText(selector string) (string, string, bool, error) {
	trimmed := strings.TrimRightFunc(selector, unicode.IsSpace)
	if trimmed == "" {
		return selector, "", false, nil
	}

	count := strings.Count(trimmed, "has-text(")
	if count == 0 {
		return selector, "", false, nil
	}
	if count > 1 {
		// Treat as literal if multiple occurrences are present.
		return selector, "", false, nil
	}

	last := strings.LastIndex(trimmed, "has-text(")
	if last < 0 {
		return selector, "", false, nil
	}

	start := last
	prefixLen := len("has-text(")
	if last > 0 && trimmed[last-1] == ':' {
		start = last - 1
		prefixLen = len(":has-text(")
	}

	if !strings.HasSuffix(trimmed, ")") {
		return selector, "", false, errors.New("inline has-text() must appear at the end of the selector")
	}

	// If the only occurrence is not at the end, it's unsupported.
	if start+prefixLen >= len(trimmed) || start+prefixLen > len(trimmed)-1 {
		return selector, "", false, errors.New("inline has-text() must appear at the end of the selector")
	}

	content := trimmed[start+prefixLen : len(trimmed)-1]

	// Strip edge quotes if they wrap the entire content and don't appear inside.
	if len(content) >= 2 {
		q := content[0]
		if (q == '"' || q == '\'') && content[len(content)-1] == q {
			if strings.Count(content, string(q)) == 2 {
				content = content[1 : len(content)-1]
			}
		}
	}

	base := strings.TrimRightFunc(trimmed[:start], unicode.IsSpace)
	return base, content, true, nil
}

func autoQuoteAttrValues(selector string) string {
	// Best-effort: if an attribute selector uses an unquoted value with spaces,
	// wrap it in double quotes (e.g. [placeholder=Enter 6-char code]).
	var out strings.Builder
	out.Grow(len(selector))
	for i := 0; i < len(selector); i++ {
		ch := selector[i]
		if ch != '[' {
			out.WriteByte(ch)
			continue
		}
		// Copy '[' and scan until matching ']'.
		start := i
		end := -1
		for j := i + 1; j < len(selector); j++ {
			if selector[j] == ']' {
				end = j
				break
			}
		}
		if end == -1 {
			out.WriteString(selector[start:])
			break
		}
		block := selector[start+1 : end]
		fixed := fixAttrBlock(block)
		out.WriteByte('[')
		out.WriteString(fixed)
		out.WriteByte(']')
		i = end
	}
	return out.String()
}

func fixAttrBlock(block string) string {
	// Only handle simple [attr=value] (no operators like ~=|=^=$=*=).
	ops := []string{"~=", "|=", "^=", "$=", "*="}
	for _, op := range ops {
		if strings.Contains(block, op) {
			return block
		}
	}
	eq := strings.IndexByte(block, '=')
	if eq == -1 {
		return block
	}
	attr := strings.TrimSpace(block[:eq])
	val := strings.TrimSpace(block[eq+1:])
	if attr == "" || val == "" {
		return block
	}
	// Already quoted?
	if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
		return block
	}
	// Only auto-quote when spaces exist and no quotes inside.
	if !strings.ContainsAny(val, " \t") {
		return block
	}
	if strings.ContainsAny(val, `"'`) {
		return block
	}
	return attr + "=\"" + val + "\""
}

func rejectUnsupportedSelector(selector, context string, allowHasTextLiteral bool) error {
	if strings.Contains(selector, ":has-text(") || strings.Contains(selector, "has-text(") {
		if allowHasTextLiteral {
			return nil
		}
		return fmt.Errorf("%s: selector uses :has-text(...), which is only supported inline at the end for click/type/hover; use --has-text there or a different selector", context)
	}
	return nil
}

func escapeLeadingPlusRegexSpec(spec string) string {
	if spec == "" {
		return spec
	}
	if strings.HasPrefix(spec, "/") {
		last := strings.LastIndex(spec, "/")
		if last > 0 {
			pattern := spec[1:last]
			flags := spec[last+1:]
			if strings.HasPrefix(pattern, "\\+") {
				return spec
			}
			if strings.HasPrefix(pattern, "+") {
				pattern = "\\+" + pattern[1:]
				return "/" + pattern + "/" + flags
			}
			return spec
		}
	}
	if strings.HasPrefix(spec, "\\+") {
		return spec
	}
	if strings.HasPrefix(spec, "+") {
		return "\\+" + spec[1:]
	}
	return spec
}

func expandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func cmdLog(args []string) error {
	fs := newFlagSet("log", "usage: cdp log <name> [\"setup script\"] [options]")
	limitFlag := fs.Int("limit", 0, "Maximum log entries to collect (<=0 for unlimited)")
	timeoutFlag := fs.Duration("timeout", 0, "Maximum time to wait for log events (0 disables)")
	levelFlag := fs.String("level", "", "Regex to filter by level/type (e.g. 'error|warning|exception')")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		fs.Usage()
		return errors.New("usage: cdp log <name> [\"setup script\"] [options]")
	}
	name := pos[0]
	script := ""
	if len(pos) > 1 {
		script = pos[1]
	}
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	limit := *limitFlag
	timeout := *timeoutFlag

	var levelFilter *regexp.Regexp
	if *levelFlag != "" {
		levelSpec := escapeLeadingPlusRegexSpec(*levelFlag)
		levelFilter, err = regexp.Compile(levelSpec)
		if err != nil {
			return fmt.Errorf("invalid --level regex: %w", err)
		}
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
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timeoutCh = timer.C
		defer timer.Stop()
	}

	limitInfo := "unlimited"
	if limit > 0 {
		limitInfo = strconv.Itoa(limit)
	}
	timeoutInfo := "none"
	if timeout > 0 {
		timeoutInfo = timeout.String()
	}
	fmt.Fprintf(os.Stderr, "Streaming console output (limit=%s, timeout=%s). Ctrl+C to stop.\n", limitInfo, timeoutInfo)

	logCount := 0
	exitReason := ""

loop:
	for {
		select {
		case <-ctx.Done():
			if exitReason == "" {
				exitReason = "context cancelled"
			}
			break loop
		case evt := <-events:
			printed, err := handleLogEvent(ctx, handle.client, evt, levelFilter)
			if err != nil {
				fmt.Fprintln(os.Stderr, "log handler:", err)
			}
			if printed {
				logCount++
			}
			if limit > 0 && logCount >= limit {
				exitReason = fmt.Sprintf("limit reached (%d entries)", limit)
				break loop
			}
		case <-timeoutCh:
			exitReason = fmt.Sprintf("timeout reached (%s)", timeout)
			break loop
		case <-sigCh:
			exitReason = "interrupted"
			cancel()
			break loop
		}
	}

	if exitReason == "" {
		exitReason = "completed"
	}
	fmt.Fprintf(os.Stderr, "Log stream ended (%s). Entries: %d\n", exitReason, logCount)
	return nil
}

func cmdKeepAlive(args []string) error {
	fs := newFlagSet("keep-alive", "usage: cdp keep-alive <name>")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp keep-alive <name>")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return errors.New("usage: cdp keep-alive <name>")
	}
	name := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
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

func handleLogEvent(ctx context.Context, client *cdp.Client, evt cdp.Event, levelFilter *regexp.Regexp) (bool, error) {
	switch evt.Method {
	case "Runtime.consoleAPICalled":
		var payload struct {
			Type string             `json:"type"`
			Args []cdp.RemoteObject `json:"args"`
		}
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return false, err
		}
		if levelFilter != nil && !levelFilter.MatchString(payload.Type) {
			return false, nil
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
		return true, nil

	case "Runtime.exceptionThrown":
		if levelFilter != nil && !levelFilter.MatchString("exception") {
			return false, nil
		}
		var payload struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception *struct {
					Description string           `json:"description"`
					Value       *json.RawMessage `json:"value"`
				} `json:"exception"`
				StackTrace *struct {
					CallFrames []struct {
						FunctionName string `json:"functionName"`
						URL          string `json:"url"`
						LineNumber   int    `json:"lineNumber"`
						ColumnNumber int    `json:"columnNumber"`
					} `json:"callFrames"`
				} `json:"stackTrace"`
			} `json:"exceptionDetails"`
		}
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return false, err
		}
		details := payload.ExceptionDetails
		desc := ""
		if details.Exception != nil {
			desc = details.Exception.Description
			if desc == "" && details.Exception.Value != nil {
				desc = string(*details.Exception.Value)
			}
		}
		if desc != "" {
			// V8 Error descriptions include the stack trace already.
			fmt.Printf("[exception] %s\n", desc)
		} else {
			fmt.Printf("[exception] %s\n", details.Text)
			// Only print callFrames manually when description is absent
			// (e.g. non-Error throw values like strings/numbers).
			if details.StackTrace != nil {
				for _, f := range details.StackTrace.CallFrames {
					fn := f.FunctionName
					if fn == "" {
						fn = "(anonymous)"
					}
					fmt.Printf("  at %s (%s:%d:%d)\n", fn, f.URL, f.LineNumber+1, f.ColumnNumber+1)
				}
			}
		}
		return true, nil

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
			return false, err
		}
		entry := payload.Entry
		if levelFilter != nil && !levelFilter.MatchString(entry.Level) {
			return false, nil
		}
		location := ""
		if entry.URL != "" {
			location = fmt.Sprintf(" (%s:%d:%d)", entry.URL, entry.Line, entry.Column)
		}
		fmt.Printf("[%s/%s] %s%s\n", entry.Source, entry.Level, entry.Text, location)
		return true, nil
	}
	return false, nil
}

func cmdNetworkLog(args []string) error {
	fs := newFlagSet("network-log", "usage: cdp network-log <name> [options]")
	dirFlag := fs.String("dir", "", "Directory for captured requests (default ./cdp-<name>-network-log)")
	urlPattern := fs.String("url", "", "Regex to match request URLs")
	methodPattern := fs.String("method", "", "Regex to match HTTP methods")
	statusPattern := fs.String("status", "", "Regex to match HTTP status codes")
	mimePattern := fs.String("mime", "", "Regex to match response Content-Type values")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp network-log <name> [options]")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return errors.New("usage: cdp network-log <name> [options]")
	}
	name := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
	}

	filters, err := buildNetworkFilters(*urlPattern, *methodPattern, *statusPattern, *mimePattern)
	if err != nil {
		return err
	}

	outputDir := *dirFlag
	if outputDir == "" {
		sessionFragment := sanitizePathFragment(name)
		if sessionFragment == "" {
			sessionFragment = "session"
		}
		outputDir = fmt.Sprintf("cdp-%s-network-log", sessionFragment)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
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

	opts := networkCaptureOptions{
		Dir:     outputDir,
		Filters: filters,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runNetworkCapture(ctx, handle.client, opts)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		cancel()
		err := <-errCh
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case err := <-errCh:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

type networkCaptureOptions struct {
	Dir     string
	Filters networkFilters
}

type networkFilters struct {
	url    *regexp.Regexp
	method *regexp.Regexp
	status *regexp.Regexp
	mime   *regexp.Regexp
}

func buildNetworkFilters(urlPattern, methodPattern, statusPattern, mimePattern string) (networkFilters, error) {
	var filters networkFilters
	var err error
	if urlPattern != "" {
		urlPattern = escapeLeadingPlusRegexSpec(urlPattern)
		filters.url, err = regexp.Compile(urlPattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --url regex: %w", err)
		}
	}
	if methodPattern != "" {
		methodPattern = escapeLeadingPlusRegexSpec(methodPattern)
		filters.method, err = regexp.Compile(methodPattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --method regex: %w", err)
		}
	}
	if statusPattern != "" {
		statusPattern = escapeLeadingPlusRegexSpec(statusPattern)
		filters.status, err = regexp.Compile(statusPattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --status regex: %w", err)
		}
	}
	if mimePattern != "" {
		mimePattern = escapeLeadingPlusRegexSpec(mimePattern)
		filters.mime, err = regexp.Compile(mimePattern)
		if err != nil {
			return filters, fmt.Errorf("invalid --mime regex: %w", err)
		}
	}
	return filters, nil
}

func (f networkFilters) match(url, method, status, mime string) bool {
	if f.url != nil && !f.url.MatchString(url) {
		return false
	}
	if f.method != nil && !f.method.MatchString(method) {
		return false
	}
	if f.status != nil && !f.status.MatchString(status) {
		return false
	}
	if f.mime != nil && !f.mime.MatchString(mime) {
		return false
	}
	return true
}

type fetchRequestPausedEvent struct {
	RequestID          string             `json:"requestId"`
	Request            fetchRequestInfo   `json:"request"`
	ResponseStatusCode *int               `json:"responseStatusCode"`
	ResponseHeaders    []fetchHeaderEntry `json:"responseHeaders"`
	RequestStage       string             `json:"requestStage"`
}

type fetchRequestInfo struct {
	URL      string                 `json:"url"`
	Method   string                 `json:"method"`
	Headers  map[string]interface{} `json:"headers"`
	PostData string                 `json:"postData"`
}

type fetchHeaderEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func runNetworkCapture(ctx context.Context, client *cdp.Client, opts networkCaptureOptions) error {
	if err := client.Call(ctx, "Network.enable", nil, nil); err != nil {
		return err
	}
	if err := client.Call(ctx, "Fetch.enable", map[string]interface{}{
		"patterns": []map[string]interface{}{
			{
				"urlPattern":   "*",
				"requestStage": "Response",
			},
		},
		"handleAuthRequests": false,
	}, nil); err != nil {
		return err
	}
	defer func() {
		disableCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client.Call(disableCtx, "Fetch.disable", nil, nil)
	}()

	var wg sync.WaitGroup
	unsubscribe := client.SubscribeEvents(func(evt cdp.Event) {
		if evt.Method != "Fetch.requestPaused" {
			return
		}
		var payload fetchRequestPausedEvent
		if err := json.Unmarshal(evt.Params, &payload); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		wg.Add(1)
		go func(event fetchRequestPausedEvent) {
			defer wg.Done()
			processFetchPaused(ctx, client, opts, event)
		}(payload)
	})
	defer func() {
		unsubscribe()
		wg.Wait()
	}()

	<-ctx.Done()
	return ctx.Err()
}

type networkCapture struct {
	Timestamp         time.Time
	RequestID         string
	URL               string
	Method            string
	Stage             string
	Status            string
	ContentType       string
	RequestHeaders    map[string]string
	ResponseHeaders   map[string]string
	RequestBody       []byte
	ResponseBody      []byte
	ResponseBodyError string
}

func processFetchPaused(ctx context.Context, client *cdp.Client, opts networkCaptureOptions, event fetchRequestPausedEvent) {
	defer continueFetchRequest(client, event.RequestID)

	url := event.Request.URL
	method := event.Request.Method
	status := "<pending>"
	if event.ResponseStatusCode != nil {
		status = strconv.Itoa(*event.ResponseStatusCode)
	}
	responseHeaders := normalizeHeaderList(event.ResponseHeaders)
	contentType := strings.ToLower(responseHeaders["content-type"])
	if !opts.Filters.match(url, method, status, contentType) {
		return
	}

	body, bodyErr := fetchResponseBody(ctx, client, event.RequestID)
	requestHeaders := sanitizeHeaderMap(event.Request.Headers)
	var requestBody []byte
	if event.Request.PostData != "" {
		requestBody = []byte(event.Request.PostData)
	}

	capture := networkCapture{
		Timestamp:         time.Now(),
		RequestID:         event.RequestID,
		URL:               url,
		Method:            method,
		Stage:             event.RequestStage,
		Status:            status,
		ContentType:       contentType,
		RequestHeaders:    requestHeaders,
		ResponseHeaders:   responseHeaders,
		RequestBody:       requestBody,
		ResponseBody:      body,
		ResponseBodyError: bodyErr,
	}
	if err := writeNetworkCapture(opts.Dir, capture); err != nil {
		fmt.Fprintf(os.Stderr, "cdp network-log: failed to write capture for %s: %v\n", event.RequestID, err)
	}
}

func fetchResponseBody(ctx context.Context, client *cdp.Client, requestID string) ([]byte, string) {
	var result struct {
		Body          string `json:"body"`
		Base64Encoded bool   `json:"base64Encoded"`
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.Call(callCtx, "Fetch.getResponseBody", map[string]interface{}{
		"requestId": requestID,
	}, &result); err != nil {
		return nil, err.Error()
	}
	if result.Body == "" {
		return nil, ""
	}
	if result.Base64Encoded {
		data, err := base64.StdEncoding.DecodeString(result.Body)
		if err != nil {
			return nil, fmt.Sprintf("decode body: %v", err)
		}
		return data, ""
	}
	return []byte(result.Body), ""
}

func continueFetchRequest(client *cdp.Client, requestID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client.Call(ctx, "Fetch.continueRequest", map[string]interface{}{
		"requestId": requestID,
	}, nil)
}

func normalizeHeaderList(headers []fetchHeaderEntry) map[string]string {
	result := make(map[string]string, len(headers))
	for _, header := range headers {
		name := strings.ToLower(strings.TrimSpace(header.Name))
		if name == "" {
			continue
		}
		result[name] = header.Value
	}
	return result
}

func sanitizeHeaderMap(headers map[string]interface{}) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		if key == "" || value == nil {
			continue
		}
		result[key] = fmt.Sprint(value)
	}
	return result
}

func writeNetworkCapture(baseDir string, capture networkCapture) error {
	dirName := formatCaptureDirName(capture)
	captureDir := filepath.Join(baseDir, dirName)
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		return err
	}

	metadata := map[string]interface{}{
		"timestamp": capture.Timestamp.Format(time.RFC3339Nano),
		"requestId": capture.RequestID,
		"url":       capture.URL,
		"method":    capture.Method,
		"stage":     capture.Stage,
		"status":    capture.Status,
	}
	if capture.ContentType != "" {
		metadata["contentType"] = capture.ContentType
	}
	if capture.ResponseBodyError != "" {
		metadata["responseBodyError"] = capture.ResponseBodyError
	}
	if err := writeJSONFile(filepath.Join(captureDir, "metadata.json"), metadata); err != nil {
		return err
	}

	reqHeaders := capture.RequestHeaders
	if reqHeaders == nil {
		reqHeaders = map[string]string{}
	}
	if err := writeJSONFile(filepath.Join(captureDir, "request-headers.json"), reqHeaders); err != nil {
		return err
	}

	respHeaders := capture.ResponseHeaders
	if respHeaders == nil {
		respHeaders = map[string]string{}
	}
	if err := writeJSONFile(filepath.Join(captureDir, "response-headers.json"), respHeaders); err != nil {
		return err
	}

	if len(capture.RequestBody) > 0 {
		if err := os.WriteFile(filepath.Join(captureDir, "request-body.bin"), capture.RequestBody, 0o644); err != nil {
			return err
		}
	}
	if len(capture.ResponseBody) > 0 {
		if err := os.WriteFile(filepath.Join(captureDir, "response-body.bin"), capture.ResponseBody, 0o644); err != nil {
			return err
		}
		if err := writeResponseBodyJSON(filepath.Join(captureDir, "response-body.json"), capture.ResponseBody); err != nil {
			return err
		}
	}
	return nil
}

func writeResponseBodyJSON(path string, body []byte) error {
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

func writeJSONFile(path string, payload interface{}) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func sanitizePathFragment(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func formatCaptureDirName(capture networkCapture) string {
	ms := capture.Timestamp.UnixNano() / int64(time.Millisecond)
	method := strings.ToUpper(strings.TrimSpace(capture.Method))
	if method == "" {
		method = "REQ"
	}
	urlFragment := shortenURLFragment(capture.URL, 96)
	return fmt.Sprintf("%d-%s-%s", ms, method, urlFragment)
}

func shortenURLFragment(raw string, limit int) string {
	fragment := normalizeURLFragment(raw)
	if limit <= 0 || len(fragment) <= limit {
		return fragment
	}
	if limit <= 6 {
		return fragment[:limit]
	}
	head := (limit - 3) / 2
	tail := limit - 3 - head
	return fragment[:head] + "..." + fragment[len(fragment)-tail:]
}

func normalizeURLFragment(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "url"
	}
	if u, err := url.Parse(clean); err == nil && u.Host != "" {
		clean = u.Host + u.Path
	} else {
		if i := strings.Index(clean, "://"); i != -1 {
			clean = clean[i+3:]
		}
		if i := strings.IndexAny(clean, "?#"); i != -1 {
			clean = clean[:i]
		}
	}
	clean = strings.SplitN(clean, "?", 2)[0]
	clean = strings.SplitN(clean, "#", 2)[0]
	clean = strings.Trim(clean, "/")
	clean = strings.ReplaceAll(clean, "/", "-")
	clean = strings.ToLower(clean)

	var b strings.Builder
	for _, r := range clean {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	fragment := strings.Trim(b.String(), "-_.")
	if fragment == "" {
		return "url"
	}
	return fragment
}

func cmdTabs(args []string) error {
	if len(args) == 0 {
		printTabsUsage()
		return errors.New("usage: cdp tabs <command> (list|switch|open|close)")
	}
	if isHelpArg(args[0]) {
		printTabsUsage()
		return nil
	}
	switch args[0] {
	case "list":
		return cmdTabsList(args[1:])
	case "switch":
		return cmdTabsSwitch(args[1:])
	case "open":
		return cmdTabsOpen(args[1:])
	case "close":
		return cmdTabsClose(args[1:])
	default:
		return fmt.Errorf("unknown tabs command %q (expected list, switch, open, or close)", args[0])
	}
}

func printTabsUsage() {
	fmt.Println("usage: cdp tabs <command> (list|switch|open|close)")
	fmt.Println("Commands:")
	fmt.Println("  list    List available tabs from a remote debugging port")
	fmt.Println("  switch  Activate a tab by index, id, or pattern")
	fmt.Println("  open    Open a new tab")
	fmt.Println("  close   Close a tab by reference or by saved session name")
	fmt.Println("Run 'cdp tabs <command> --help' for details.")
}

func cmdTabsList(args []string) error {
	fs := newFlagSet("tabs list", "usage: cdp tabs list [--host --port] [--plain] [--pretty]")
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", portDefault(9222), "DevTools port")
	plain := fs.Bool("plain", false, "Output plain text table instead of JSON")
	pretty := fs.Bool("pretty", defaultPretty(), "Pretty print JSON output")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("unexpected argument: %s", pos[0])
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	tabs, err := fetchTabs(ctx, *host, *port)
	if err != nil {
		return err
	}

	if *plain {
		if len(tabs) == 0 {
			fmt.Println("No tabs found")
			return nil
		}
		fmt.Printf("%-4s %-40s %s\n", "#", "TITLE", "URL")
		for i, tab := range tabs {
			title := tab.Title
			if strings.TrimSpace(title) == "" {
				title = "<untitled>"
			}
			fmt.Printf("%-4d %-40s %s\n", i+1, abbreviate(title, 40), tab.URL)
		}
		return nil
	}

	output, err := format.JSON(tabs, *pretty, -1)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func cmdTabsSwitch(args []string) error {
	fs := newFlagSet("tabs switch", "usage: cdp tabs switch <index|id|pattern>")
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", portDefault(9222), "DevTools port")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: cdp tabs switch <index|id|pattern>")
	}
	targetRef := pos[0]

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	tabs, err := fetchTabs(ctx, *host, *port)
	if err != nil {
		return err
	}
	if len(tabs) == 0 {
		return errors.New("no tabs available (use 'cdp tabs list' to double-check)")
	}

	tab, err := matchTab(tabs, targetRef)
	if err != nil {
		return err
	}

	if err := cdp.ActivateTarget(ctx, *host, *port, tab.ID); err != nil {
		return err
	}
	title := tab.Title
	if strings.TrimSpace(title) == "" {
		title = "<untitled>"
	}
	fmt.Printf("Activated tab: %s (%s)\n", abbreviate(title, 60), tab.URL)
	return nil
}

func cmdTabsOpen(args []string) error {
	fs := newFlagSet("tabs open", "usage: cdp tabs open <url>")
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", portDefault(9222), "DevTools port")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	activate := fs.Bool("activate", true, "Activate the tab after opening")
	pageURL, flagArgs, err := splitTabsOpenArgs(args)
	if err != nil {
		return err
	}
	fs.Parse(flagArgs)
	if fs.NArg() != 0 {
		return errors.New("usage: cdp tabs open <url>")
	}
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		return errors.New("url cannot be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	tab, err := cdp.CreateTarget(ctx, *host, *port, pageURL)
	if err != nil {
		return err
	}
	if tab.URL == "" {
		tab.URL = pageURL
	}
	title := tab.Title
	if strings.TrimSpace(title) == "" {
		title = "<untitled>"
	}
	if *activate {
		if err := cdp.ActivateTarget(ctx, *host, *port, tab.ID); err != nil {
			return err
		}
		fmt.Printf("Opened and activated tab: %s (%s)\n", abbreviate(title, 60), tab.URL)
		return nil
	}
	fmt.Printf("Opened tab: %s (%s)\n", abbreviate(title, 60), tab.URL)
	return nil
}

func cmdTabsClose(args []string) error {
	fs := newFlagSet("tabs close", "usage: cdp tabs close <index|id|pattern> [--host --port]\nor:    cdp tabs close --session <name>")
	host := fs.String("host", "127.0.0.1", "DevTools host")
	port := fs.Int("port", portDefault(9222), "DevTools port")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	sessionName := fs.String("session", "", "Close tab by saved session name")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}

	if *sessionName != "" {
		if len(pos) != 0 {
			return errors.New("usage: cdp tabs close --session <name>")
		}
		st, err := store.Load()
		if err != nil {
			return err
		}
		session, ok := st.Get(*sessionName)
		if !ok {
			return fmt.Errorf("unknown session %q", *sessionName)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()

		client, updated, err := attachSession(ctx, session)
		if err != nil {
			return err
		}
		defer client.Close()

		if err := client.Call(ctx, "Target.closeTarget", map[string]interface{}{"targetId": updated.TargetID}, nil); err != nil {
			return err
		}
		title := updated.Title
		if strings.TrimSpace(title) == "" {
			title = "<untitled>"
		}
		fmt.Printf("Closed tab for session %s: %s (%s)\n", *sessionName, abbreviate(title, 60), updated.URL)
		return nil
	}

	if len(pos) != 1 {
		return errors.New("usage: cdp tabs close <index|id|pattern>")
	}
	targetRef := pos[0]

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	tabs, err := fetchTabs(ctx, *host, *port)
	if err != nil {
		return err
	}
	if len(tabs) == 0 {
		return errors.New("no tabs available (use 'cdp tabs list' to double-check)")
	}
	tab, err := matchTab(tabs, targetRef)
	if err != nil {
		return err
	}
	if err := cdp.CloseTarget(ctx, *host, *port, tab.ID); err != nil {
		return err
	}
	title := tab.Title
	if strings.TrimSpace(title) == "" {
		title = "<untitled>"
	}
	fmt.Printf("Closed tab: %s (%s)\n", abbreviate(title, 60), tab.URL)
	return nil
}

func fetchTabs(ctx context.Context, host string, port int) ([]cdp.TargetInfo, error) {
	targets, err := cdp.ListTargets(ctx, host, port)
	if err != nil {
		return nil, err
	}
	tabs := make([]cdp.TargetInfo, 0, len(targets))
	for _, target := range targets {
		if target.Type == "page" {
			tabs = append(tabs, target)
		}
	}
	return tabs, nil
}

func matchTab(tabs []cdp.TargetInfo, ref string) (cdp.TargetInfo, error) {
	if idx, err := strconv.Atoi(ref); err == nil {
		if idx <= 0 || idx > len(tabs) {
			return cdp.TargetInfo{}, fmt.Errorf("index %d is out of range (tabs available: %d)", idx, len(tabs))
		}
		return tabs[idx-1], nil
	}
	for _, tab := range tabs {
		if tab.ID == ref {
			return tab, nil
		}
	}
	lowerRef := strings.ToLower(ref)
	matches := make([]cdp.TargetInfo, 0, 2)
	for _, tab := range tabs {
		if strings.Contains(strings.ToLower(tab.URL), lowerRef) || strings.Contains(strings.ToLower(tab.Title), lowerRef) {
			matches = append(matches, tab)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return cdp.TargetInfo{}, fmt.Errorf("pattern %q matches multiple tabs; be more specific", ref)
	}
	return cdp.TargetInfo{}, fmt.Errorf("no tab matches %q (try 'cdp tabs list')", ref)
}

func cmdTargets(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		fmt.Println("usage: cdp targets")
		return nil
	}
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

func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	flagInfo := make(map[string]bool)
	fs.VisitAll(func(f *flag.Flag) {
		isBool := false
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			isBool = true
		}
		flagInfo[f.Name] = isBool
	})
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			name, hasValue := splitFlagName(arg)
			if isBool, ok := flagInfo[name]; ok {
				flags = append(flags, arg)
				if hasValue {
					continue
				}
				if isBool {
					if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
						flags = append(flags, args[i+1])
						i++
					}
					continue
				}
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %s requires a value", arg)
				}
				flags = append(flags, args[i+1])
				i++
				continue
			}
		}
		positionals = append(positionals, arg)
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positionals, nil
}

func splitTabsOpenArgs(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New("usage: cdp tabs open <url>")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		return "", nil, errors.New("usage: cdp tabs open <url>")
	}
	var url string
	flags := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			name, hasValue := splitFlagName(arg)
			if hasValue {
				continue
			}
			switch name {
			case "host", "port", "timeout":
				if i+1 >= len(args) {
					return "", nil, fmt.Errorf("flag %s requires a value", arg)
				}
				flags = append(flags, args[i+1])
				i++
			case "activate":
				if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
					flags = append(flags, args[i+1])
					i++
				}
			}
			continue
		}
		if url != "" {
			return "", nil, fmt.Errorf("unexpected argument: %s", arg)
		}
		url = arg
	}
	if url == "" {
		return "", nil, errors.New("usage: cdp tabs open <url>")
	}
	return url, flags, nil
}

func splitFlagName(arg string) (string, bool) {
	name := strings.TrimLeft(arg, "-")
	if name == "" {
		return "", false
	}
	if idx := strings.Index(name, "="); idx != -1 {
		return name[:idx], true
	}
	return name, false
}

func waitForReadyState(ctx context.Context, client *cdp.Client, poll time.Duration) error {
	return waitForCondition(ctx, client, `document.readyState === "complete"`, "document.readyState == 'complete'", poll)
}

func waitForSelector(ctx context.Context, client *cdp.Client, selector string, poll time.Duration) error {
	expression := fmt.Sprintf(`(() => {
        return document.querySelector(%s) !== null;
    })()`, strconv.Quote(selector))
	return waitForCondition(ctx, client, expression, fmt.Sprintf("selector %s", selector), poll)
}

func waitForSelectorVisible(ctx context.Context, client *cdp.Client, selector string, poll time.Duration) error {
	expression := fmt.Sprintf(`(() => {
        const el = document.querySelector(%s);
        if (!el) { return false; }
        const style = window.getComputedStyle(el);
        if (style && (style.display === "none" || style.visibility === "hidden" || style.opacity === "0")) {
            return false;
        }
        const rect = el.getBoundingClientRect();
        return rect.width > 0 && rect.height > 0;
    })()`, strconv.Quote(selector))
	return waitForCondition(ctx, client, expression, fmt.Sprintf("visible selector %s", selector), poll)
}

func waitForCondition(ctx context.Context, client *cdp.Client, expression, description string, poll time.Duration) error {
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		ok, err := evalBool(ctx, client, expression)
		if err == nil && ok {
			return nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("timeout waiting for %s", description)
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func evalBool(ctx context.Context, client *cdp.Client, expression string) (bool, error) {
	value, err := client.Evaluate(ctx, expression)
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("unexpected wait result type %T", value)
	}
	return result, nil
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

func cmdDisconnect(args []string) error {
	fs := newFlagSet("disconnect", "usage: cdp disconnect <name>")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: cdp disconnect <name>")
	}
	name := fs.Arg(0)

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

func envDefaultPort() (int, bool) {
	raw := strings.TrimSpace(os.Getenv("CDP_PORT"))
	if raw == "" {
		return 0, false
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val <= 0 {
		return 0, false
	}
	return val, true
}

func portDefault(fallback int) int {
	if val, ok := envDefaultPort(); ok {
		return val
	}
	return fallback
}

func readScriptFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

func newFlagSet(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), usage)
		if flagHasOptions(fs) {
			fmt.Fprintln(fs.Output(), "\nOptions:")
			fs.PrintDefaults()
		}
	}
	return fs
}

func flagHasOptions(fs *flag.FlagSet) bool {
	has := false
	fs.VisitAll(func(*flag.Flag) {
		has = true
	})
	return has
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help"
}
