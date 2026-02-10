package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

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
