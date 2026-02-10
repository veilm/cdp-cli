package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/veilm/cdp-cli/internal/store"
)

// buildFilteredTargetExpr constructs a JS expression for element targeting.
// When hasText or attValue are specified, it builds a querySelectorAll chain
// with .hasText()/.hasAttValue() filters. Otherwise returns the selector(s) as-is.
func buildFilteredTargetExpr(selectors []string, hasText, attValue string) string {
	if hasText == "" && attValue == "" {
		if len(selectors) == 1 {
			return strconv.Quote(selectors[0])
		}
		b, _ := json.Marshal(selectors)
		return string(b)
	}

	addFilters := func(expr string) string {
		if hasText != "" {
			expr += fmt.Sprintf(`.hasText(%s)`, strconv.Quote(hasText))
		}
		if attValue != "" {
			expr += fmt.Sprintf(`.hasAttValue(%s)`, strconv.Quote(attValue))
		}
		return expr
	}

	if len(selectors) == 1 {
		return addFilters(fmt.Sprintf(`document.querySelectorAll(%s)`, strconv.Quote(selectors[0])))
	}

	// Multiple selectors: try each in order to preserve priority (e.g. "button" before "div").
	var b strings.Builder
	b.WriteString("(function(){var r;")
	for i, sel := range selectors {
		expr := addFilters(fmt.Sprintf(`document.querySelectorAll(%s)`, strconv.Quote(sel)))
		fmt.Fprintf(&b, "r=%s;", expr)
		if i < len(selectors)-1 {
			b.WriteString("if(r.length)return r;")
		}
	}
	b.WriteString("return r;})()")
	return b.String()
}

func cmdInject(args []string) error {
	fs := newFlagSet("inject", "usage: cdp inject <name> [--force]")
	force := fs.Bool("force", false, "Force re-injection even if WebNav is already present")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp inject <name> [--force]")
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
		return errors.New("usage: cdp inject <name> [--force]")
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

	if err := injectWebNav(ctx, handle.client, *force); err != nil {
		return err
	}
	fmt.Printf("Injected WebNav helpers into %s\n", name)
	return nil
}

func cmdClick(args []string) error {
	fs := newFlagSet("click", "usage: cdp click <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]\n(also supports inline :has-text(...) at the end of the selector)")
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	count := fs.Int("count", 1, "Number of clicks to perform")
	submitWaitMS := fs.Int("submit-wait-ms", 700, "If clicking a submit button inside a form, wait N ms before returning (0 disables)")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp click <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
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
		return errors.New("usage: cdp click <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
	}
	name := pos[0]
	selector := ""
	if len(pos) >= 2 {
		selector = pos[1]
	}
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	inlineHasText := ""
	hasInline := false
	if selector != "" {
		selector, inlineHasText, hasInline, err = parseInlineHasText(selector)
		if err != nil {
			return err
		}
		if err := rejectUnsupportedSelector(selector, "click", true); err != nil {
			return err
		}
	} else if *hasText == "" {
		return errors.New("usage: cdp click <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
	}
	if *count < 1 {
		return errors.New("--count must be >= 1")
	}
	selectors := []string{}
	if selector != "" {
		selectors = append(selectors, autoQuoteAttrValues(selector))
	} else {
		selectors = append(selectors, "button", "div")
	}
	for _, sel := range selectors {
		if err := rejectUnsupportedSelector(sel, "click", true); err != nil {
			return err
		}
	}
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	targetExpr := buildFilteredTargetExpr(selectors, hasTextValue, attValueValue)
	expression := fmt.Sprintf(`window.WebNavClick(%s, %d)`, targetExpr, *count)

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
	usedSelector := selector
	if m, ok := value.(map[string]interface{}); ok {
		if sel, _ := m["selector"].(string); sel != "" {
			usedSelector = sel
		}
	}
	if *count == 1 {
		fmt.Printf("Clicked: %s\n", usedSelector)
	} else {
		fmt.Printf("Clicked: %s (x%d)\n", usedSelector, *count)
	}
	return nil
}

func cmdHover(args []string) error {
	fs := newFlagSet("hover", "usage: cdp hover <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX]\n(also supports inline :has-text(...) at the end of the selector)")
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	hold := fs.Duration("hold", 0, "Optional time to wait after hovering")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp hover <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX]")
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
		return errors.New("usage: cdp hover <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX]")
	}
	name := pos[0]
	selector := ""
	if len(pos) >= 2 {
		selector = pos[1]
	}
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	inlineHasText := ""
	hasInline := false
	if selector != "" {
		selector, inlineHasText, hasInline, err = parseInlineHasText(selector)
		if err != nil {
			return err
		}
		if err := rejectUnsupportedSelector(selector, "hover", true); err != nil {
			return err
		}
	} else if *hasText == "" {
		return errors.New("usage: cdp hover <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX]")
	}
	selectors := []string{}
	if selector != "" {
		selectors = append(selectors, autoQuoteAttrValues(selector))
	} else {
		selectors = append(selectors, "div")
	}
	for _, sel := range selectors {
		if err := rejectUnsupportedSelector(sel, "hover", true); err != nil {
			return err
		}
	}
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	targetExpr := buildFilteredTargetExpr(selectors, hasTextValue, attValueValue)
	expression := fmt.Sprintf(`window.WebNavHover(%s)`, targetExpr)

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	if *hold > 0 {
		time.Sleep(*hold)
	}
	usedSelector := selector
	if m, ok := value.(map[string]interface{}); ok {
		if sel, _ := m["selector"].(string); sel != "" {
			usedSelector = sel
		}
	}
	fmt.Printf("Hovered: %s\n", usedSelector)
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	delayMS := delay.Milliseconds()
	expression := fmt.Sprintf(`window.WebNavDrag(%s, %s, %d, %d, %d)`, strconv.Quote(fromSelector), strconv.Quote(toSelector), *fromIndex, *toIndex, delayMS)

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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	delayMS := delay.Milliseconds()
	expression := fmt.Sprintf(`window.WebNavGesture(%s, %s, %d)`, strconv.Quote(selector), pointsJSON.String(), delayMS)

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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	if *element != "" {
		expression := fmt.Sprintf(`window.WebNavFocus(%s)`, strconv.Quote(*element))
		if _, err := handle.client.Evaluate(ctx, expression); err != nil {
			return err
		}
	}

	if !*useCDP {
		expression := fmt.Sprintf(`window.WebNavKey(%s)`, strconv.Quote(spec))
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
	fs := newFlagSet("type", "usage: cdp type <name> [\".selector\"] \"text\" [--has-text REGEX] [--att-value REGEX]\n(also supports inline :has-text(...) at the end of the selector)")
	appendText := fs.Bool("append", false, "Append text instead of replacing")
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 0 {
		fs.Usage()
		return errors.New("usage: cdp type <name> [\".selector\"] \"text\" [--has-text REGEX] [--att-value REGEX]")
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
		return errors.New("usage: cdp type <name> [\".selector\"] \"text\" [--has-text REGEX] [--att-value REGEX]")
	}
	name := pos[0]
	selector := ""
	text := ""
	if len(pos) == 2 {
		if *hasText == "" {
			return errors.New("usage: cdp type <name> [\".selector\"] \"text\" [--has-text REGEX] [--att-value REGEX]")
		}
		text = pos[1]
	} else {
		selector = pos[1]
		text = pos[2]
	}
	if len(pos) > 3 {
		return fmt.Errorf("unexpected argument: %s", pos[3])
	}
	inlineHasText := ""
	hasInline := false
	if selector != "" {
		selector, inlineHasText, hasInline, err = parseInlineHasText(selector)
		if err != nil {
			return err
		}
		if err := rejectUnsupportedSelector(selector, "type", true); err != nil {
			return err
		}
	}
	selectors := []string{}
	if selector != "" {
		selectors = append(selectors, autoQuoteAttrValues(selector))
	} else {
		selectors = append(selectors, "input", "textarea")
	}
	for _, sel := range selectors {
		if err := rejectUnsupportedSelector(sel, "type", true); err != nil {
			return err
		}
	}
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	targetExpr := buildFilteredTargetExpr(selectors, hasTextValue, attValueValue)
	expression := fmt.Sprintf(`window.WebNavTypePrepare(%s, %s, %t)`, targetExpr, strconv.Quote(text), *appendText)

	value, err := handle.client.Evaluate(ctx, expression)
	if err != nil {
		return err
	}
	state, ok := value.(map[string]interface{})
	if !ok || state["found"] != true {
		return errors.New("selector not found")
	}
	usedSelector := selector
	if sel, _ := state["selector"].(string); sel != "" {
		usedSelector = sel
	}
	if handled, _ := state["handled"].(bool); handled {
		fmt.Printf("Typed into: %s\n", usedSelector)
		return nil
	}
	editable, _ := state["editable"].(bool)
	if editable {
		if err := handle.client.Call(ctx, "Input.insertText", map[string]interface{}{
			"text": text,
		}, nil); err != nil {
			return err
		}
		fmt.Printf("Typed into: %s\n", usedSelector)
		return nil
	}

	fallback := fmt.Sprintf(`window.WebNavTypeFallback(%s, %s, %t)`, targetExpr, strconv.Quote(text), *appendText)
	fallbackValue, err := handle.client.Evaluate(ctx, fallback)
	if err != nil {
		return err
	}
	if m, ok := fallbackValue.(map[string]interface{}); ok {
		if okVal, _ := m["ok"].(bool); !okVal {
			return errors.New("selector not found")
		}
		if sel, _ := m["selector"].(string); sel != "" {
			usedSelector = sel
		}
	}
	fmt.Printf("Typed into: %s\n", usedSelector)
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	yJS := strconv.FormatFloat(scrollY, 'f', -1, 64)
	xJS := strconv.FormatFloat(*scrollX, 'f', -1, 64)
	expression := fmt.Sprintf(`window.WebNavScroll(%s, %s, %s, %t)`, yJS, xJS, strconv.Quote(*element), *emit)

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
