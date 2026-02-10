package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

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
