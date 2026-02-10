package cli

import (
	"errors"
	"fmt"
	"strings"
)

func splitTabsOpenArgs(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New("usage: cdp tabs open <url>")
	}
	if len(args) == 1 && isHelpArg(args[0]) {
		return "", nil, errors.New("usage: cdp tabs open <url>")
	}
	var url string
	flags := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			name, hasValue := splitFlagName(arg)
			if hasValue {
				continue
			}
			switch name {
			case "host", "port", "timeout":
				if i+1 >= len(args) {
					return "", nil, fmt.Errorf("flag %s requires a value", arg)
				}
				flags = append(flags, args[i+1])
				i++
			case "activate":
				if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
					flags = append(flags, args[i+1])
					i++
				}
			}
			continue
		}
		if url != "" {
			return "", nil, fmt.Errorf("unexpected argument: %s", arg)
		}
		url = arg
	}
	if url == "" {
		return "", nil, errors.New("usage: cdp tabs open <url>")
	}
	return url, flags, nil
}
