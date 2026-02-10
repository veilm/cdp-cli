package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/veilm/cdp-cli/internal/format"
	"github.com/veilm/cdp-cli/internal/store"
)

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
	if err := rejectUnsupportedSelector(selector, "dom", false); err != nil {
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
	if err := rejectUnsupportedSelector(selector, "styles", false); err != nil {
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
	if err := rejectUnsupportedSelector(selector, "rect", false); err != nil {
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
