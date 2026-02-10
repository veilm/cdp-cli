package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

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

func abbreviate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
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
