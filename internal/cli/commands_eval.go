package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/veilm/cdp-cli/internal/format"
	"github.com/veilm/cdp-cli/internal/store"
)

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
