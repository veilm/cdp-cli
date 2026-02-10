package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/veilm/cdp-cli/internal/cdp"
	"github.com/veilm/cdp-cli/internal/format"
	"github.com/veilm/cdp-cli/internal/store"
)

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
