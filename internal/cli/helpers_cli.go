package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	flagInfo := make(map[string]bool)
	fs.VisitAll(func(f *flag.Flag) {
		isBool := false
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			isBool = true
		}
		flagInfo[f.Name] = isBool
	})
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			name, hasValue := splitFlagName(arg)
			if isBool, ok := flagInfo[name]; ok {
				flags = append(flags, arg)
				if hasValue {
					continue
				}
				if isBool {
					if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
						flags = append(flags, args[i+1])
						i++
					}
					continue
				}
				if i+1 >= len(args) {
					return nil, fmt.Errorf("flag %s requires a value", arg)
				}
				flags = append(flags, args[i+1])
				i++
				continue
			}
		}
		positionals = append(positionals, arg)
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positionals, nil
}

func splitFlagName(arg string) (string, bool) {
	name := strings.TrimLeft(arg, "-")
	if name == "" {
		return "", false
	}
	if idx := strings.Index(name, "="); idx != -1 {
		return name[:idx], true
	}
	return name, false
}

func newFlagSet(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.SetOutput(os.Stdout)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), usage)
		if flagHasOptions(fs) {
			fmt.Fprintln(fs.Output(), "\nOptions:")
			fs.PrintDefaults()
		}
	}
	return fs
}

func flagHasOptions(fs *flag.FlagSet) bool {
	has := false
	fs.VisitAll(func(*flag.Flag) {
		has = true
	})
	return has
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help"
}
