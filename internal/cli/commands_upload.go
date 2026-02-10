package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/veilm/cdp-cli/internal/store"
)

func cmdUpload(args []string) error {
	fs := newFlagSet("upload", "usage: cdp upload --session <name> \"input[type=file]\" <file1> [file2 ...] [--wait]")
	sessionFlag := addSessionFlag(fs)
	waitFlag := fs.Bool("wait", false, "Wait for the selector to exist before uploading")
	poll := fs.Duration("poll", 200*time.Millisecond, "Polling interval when using --wait")
	timeout := fs.Duration("timeout", 10*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("missing selector and files")
	}
	selector := pos[0]
	filesRaw := pos[1:]
	if err := rejectUnsupportedSelector(selector, "upload", false); err != nil {
		return err
	}
	name, err := resolveSessionName(*sessionFlag)
	if err != nil {
		fs.Usage()
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
