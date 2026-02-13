package cli

import (
	"os"
	"strconv"
	"strings"
)

func defaultPretty() bool {
	val := strings.ToLower(strings.TrimSpace(os.Getenv("CDP_PRETTY")))
	switch val {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func envDefaultPort() (int, bool) {
	raw := strings.TrimSpace(os.Getenv("CDP_PORT"))
	if raw == "" {
		return 0, false
	}
	val, err := strconv.Atoi(raw)
	if err != nil || val <= 0 {
		return 0, false
	}
	return val, true
}

func portDefault(fallback int) int {
	if val, ok := envDefaultPort(); ok {
		return val
	}
	return fallback
}
