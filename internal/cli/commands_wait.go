package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/veilm/cdp-cli/internal/store"
)

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
