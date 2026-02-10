package cli

import (
	"errors"
	"fmt"
	"sort"

	"github.com/veilm/cdp-cli/internal/store"
)

func cmdTargets(args []string) error {
	if len(args) == 1 && isHelpArg(args[0]) {
		fmt.Println("usage: cdp targets")
		return nil
	}
	if len(args) != 0 {
		return errors.New("usage: cdp targets")
	}
	st, err := store.Load()
	if err != nil {
		return err
	}
	sessions := st.List()
	if len(sessions) == 0 {
		fmt.Println("No saved sessions")
		return nil
	}
	names := make([]string, 0, len(sessions))
	for name := range sessions {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("%-12s %-6s %-30s %s\n", "NAME", "PORT", "TITLE", "URL")
	for _, name := range names {
		session := sessions[name]
		fmt.Printf("%-12s %-6d %-30s %s\n", name, session.Port, abbreviate(session.Title, 30), session.URL)
	}
	return nil
}
