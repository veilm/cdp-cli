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

func cropForTTY(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "[...]"
}

func isBareTagSelector(selector string) bool {
	s := strings.TrimSpace(selector)
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (i > 0 && ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

// buildFilteredTargetExpr constructs a JS expression for element targeting.
// When hasText or attValue are specified, it builds a querySelectorAll chain
// with .hasText()/.hasAttValue() filters. Otherwise returns the selector(s) as-is.
func buildFilteredTargetExpr(selectors []string, hasText, attValue string, preferInner bool) string {
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
		if preferInner {
			expr += `.preferInner()`
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
	fs := newFlagSet("inject", "usage: cdp inject --session <name> [--force]")
	sessionFlag := addSessionFlag(fs)
	force := fs.Bool("force", false, "Force re-injection even if WebNav is already present")
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

	if err := injectWebNav(ctx, handle.client, *force); err != nil {
		return err
	}
	fmt.Printf("Injected WebNav helpers into %s\n", name)
	return nil
}

func cmdClick(args []string) error {
	fs := newFlagSet("click", "usage: cdp click --session <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]\n(also supports inline :has-text(...) at the end of the selector)")
	sessionFlag := addSessionFlag(fs)
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	preferInner := fs.String("prefer-inner", "auto", "Prefer inner matches when using --has-text/--att-value (yes|no|auto)")
	count := fs.Int("count", 1, "Number of clicks to perform")
	submitWaitMS := fs.Int("submit-wait-ms", 700, "If clicking a submit button inside a form, wait N ms before returning (0 disables)")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	selector := ""
	if len(pos) >= 1 {
		selector = pos[0]
	}
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
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
		return errors.New("usage: cdp click --session <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX] [--count N] [--submit-wait-ms N]")
	}
	if *count < 1 {
		return errors.New("--count must be >= 1")
	}
	selectors := []string{}
	if selector != "" {
		selectors = append(selectors, autoQuoteAttrValues(selector))
	} else {
		// Default element types when a selector isn't provided.
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

	preferInnerMode := strings.ToLower(strings.TrimSpace(*preferInner))
	if preferInnerMode == "" {
		preferInnerMode = "auto"
	}
	if preferInnerMode != "yes" && preferInnerMode != "no" && preferInnerMode != "auto" {
		return errors.New("--prefer-inner must be one of: yes, no, auto")
	}
	usePreferInner := false
	if hasTextValue != "" || attValueValue != "" {
		switch preferInnerMode {
		case "yes":
			usePreferInner = true
		case "no":
			usePreferInner = false
		case "auto":
			usePreferInner = selector == "" || isBareTagSelector(selector)
		}
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	targetExpr := buildFilteredTargetExpr(selectors, hasTextValue, attValueValue, usePreferInner)

	readOpts := map[string]interface{}{
		"waitMs":     0,
		"hasText":    "",
		"attValue":   "",
		"classLimit": 3,
	}
	readOptsJSON, _ := json.Marshal(readOpts)

	expression := fmt.Sprintf(`window.WebNavClickWithRead(%s, %d, %s)`, targetExpr, *count, string(readOptsJSON))
	raw, err := handle.client.EvaluateRaw(ctx, expression, false)
	if err != nil {
		return err
	}
	valueAny, err := handle.client.RemoteObjectValue(ctx, raw.Result)
	if err != nil {
		return err
	}
	value, ok := valueAny.(map[string]interface{})
	if !ok {
		return fmt.Errorf("unexpected WebNavClickWithRead result type %T", valueAny)
	}

	beforeText := ""
	if before, ok := value["before"].(map[string]interface{}); ok {
		if linesAny, ok := before["lines"].([]interface{}); ok {
			lines := make([]string, 0, len(linesAny))
			for _, v := range linesAny {
				if s, ok := v.(string); ok {
					lines = append(lines, s)
				} else if v != nil {
					lines = append(lines, fmt.Sprint(v))
				}
			}
			beforeText = strings.Join(lines, "\n")
		}
	}
	afterText := ""
	if after, ok := value["after"].(map[string]interface{}); ok {
		if linesAny, ok := after["lines"].([]interface{}); ok {
			lines := make([]string, 0, len(linesAny))
			for _, v := range linesAny {
				if s, ok := v.(string); ok {
					lines = append(lines, s)
				} else if v != nil {
					lines = append(lines, fmt.Sprint(v))
				}
			}
			afterText = strings.Join(lines, "\n")
		}
	}

	beforeDisp := cropForTTY(beforeText, 300)

	if *submitWaitMS > 0 {
		if submit, _ := value["submitForm"].(bool); submit {
			time.Sleep(time.Duration(*submitWaitMS) * time.Millisecond)
		}
	}

	tag, _ := value["tagName"].(string)
	if tag == "" {
		tag = "element"
	}
	if *count == 1 {
		fmt.Printf("Clicked %s:\n", tag)
	} else {
		fmt.Printf("Clicked %s %d times:\n", tag, *count)
	}
	if strings.TrimSpace(beforeDisp) != "" {
		fmt.Print(beforeDisp)
		if !strings.HasSuffix(beforeDisp, "\n") {
			fmt.Print("\n")
		}
	}

	afterDisp := cropForTTY(afterText, 300)
	if beforeDisp != afterDisp && strings.TrimSpace(afterDisp) != "" {
		fmt.Print("after the click, element updated to:\n")
		fmt.Print(afterDisp)
		if !strings.HasSuffix(afterDisp, "\n") {
			fmt.Print("\n")
		}
	}
	return nil
}

func cmdHover(args []string) error {
	fs := newFlagSet("hover", "usage: cdp hover --session <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX]\n(also supports inline :has-text(...) at the end of the selector)")
	sessionFlag := addSessionFlag(fs)
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	hold := fs.Duration("hold", 0, "Optional time to wait after hovering")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	selector := ""
	if len(pos) >= 1 {
		selector = pos[0]
	}
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
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
		return errors.New("usage: cdp hover --session <name> [\".selector\"] [--has-text REGEX] [--att-value REGEX]")
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	targetExpr := buildFilteredTargetExpr(selectors, hasTextValue, attValueValue, false)
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
	fs := newFlagSet("drag", "usage: cdp drag --session <name> \".from\" \".to\"")
	sessionFlag := addSessionFlag(fs)
	fromIndex := fs.Int("from-index", 0, "Index within the source selector (0-based)")
	toIndex := fs.Int("to-index", 0, "Index within the target selector (0-based)")
	delay := fs.Duration("delay", 0, "Delay between drag events (e.g. 50ms)")
	timeout := fs.Duration("timeout", 8*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: cdp drag --session <name> \".from\" \".to\"")
	}
	fromSelector := pos[0]
	toSelector := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
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
	usage := "usage: cdp gesture --session <name> \".selector\" \"x1,y1 x2,y2 ...\"  (draw, swipe, slide, trace)"
	fs := newFlagSet("gesture", usage+"\n\nPress-move-release along a path within an element.\nCoordinates are relative (0-1) to the element's bounding box.\n\nExamples:\n  cdp gesture mgr \"canvas\" \"0.1,0.5 0.9,0.5\"        # horizontal stroke\n  cdp gesture mgr \".slider\" \"0.0,0.5 1.0,0.5\"        # slide fully right\n  cdp gesture mgr \".pad\" \"0.2,0.2 0.8,0.2 0.8,0.8\"   # L-shaped path")
	sessionFlag := addSessionFlag(fs)
	delay := fs.Duration("delay", 50*time.Millisecond, "Delay between pointer events")
	timeout := fs.Duration("timeout", 12*time.Second, "Command timeout")
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
	selector := pos[0]
	pathStr := pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument: %s", pos[2])
	}
	if err := rejectUnsupportedSelector(selector, "gesture", false); err != nil {
		return err
	}
	name, err := resolveSessionName(*sessionFlag)
	if err != nil {
		fs.Usage()
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
	usage := "usage: cdp key --session <name> KEYS [--element \".selector\"] [--cdp]"
	fs := newFlagSet("key", usage+"\n\nSend a key press. KEYS is key names joined by + for combos.\n\nExamples:\n  cdp key mgr Enter\n  cdp key mgr Ctrl+c\n  cdp key mgr Ctrl+Shift+s\n  cdp key mgr ArrowDown\n\nKey names: Enter, Escape, Tab, Backspace, Delete, Space, ArrowUp/Down/Left/Right, Home, End, PageUp, PageDown, F1-F12, Ctrl, Shift, Alt, Meta, or any character.")
	sessionFlag := addSessionFlag(fs)
	element := fs.String("element", "", "Focus this element before sending the key")
	useCDP := fs.Bool("cdp", false, "Use CDP Input.dispatchKeyEvent instead of JS KeyboardEvent")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return errors.New(usage)
	}
	spec := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
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
	fs := newFlagSet("type", "usage: cdp type --session <name> [\".selector\"] \"text\" [--has-text REGEX] [--att-value REGEX]\n(also supports inline :has-text(...) at the end of the selector)")
	sessionFlag := addSessionFlag(fs)
	appendText := fs.Bool("append", false, "Append text instead of replacing")
	hasText := fs.String("has-text", "", "Only match elements whose text matches this regex (JS RegExp; accepts /pat/flags or pat)")
	attValue := fs.String("att-value", "", "Only match elements with at least one attribute value matching this regex (JS RegExp; accepts /pat/flags or pat)")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	selector := ""
	text := ""
	if len(pos) == 1 {
		if *hasText == "" {
			return errors.New("usage: cdp type --session <name> [\".selector\"] \"text\" [--has-text REGEX] [--att-value REGEX]")
		}
		text = pos[0]
	} else {
		if len(pos) < 2 {
			return errors.New("missing text")
		}
		selector = pos[0]
		text = pos[1]
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

	if err := ensureWebNavInjected(ctx, handle.client); err != nil {
		return err
	}

	targetExpr := buildFilteredTargetExpr(selectors, hasTextValue, attValueValue, false)
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
	fs := newFlagSet("scroll", "usage: cdp scroll --session <name> <yPx> [--x <xPx>] [--element \".selector\"] [--emit]")
	sessionFlag := addSessionFlag(fs)
	scrollX := fs.Float64("x", 0, "Horizontal scroll delta in pixels (can be negative)")
	element := fs.String("element", "", "Scroll inside an element matched by selector")
	emit := fs.Bool("emit", true, "Dispatch scroll events after scrolling")
	timeout := fs.Duration("timeout", 5*time.Second, "Command timeout")
	if len(args) == 1 && isHelpArg(args[0]) {
		fs.Usage()
		return nil
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return errors.New("missing yPx")
	}
	yStr := pos[0]
	if len(pos) > 1 {
		return fmt.Errorf("unexpected argument: %s", pos[1])
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
