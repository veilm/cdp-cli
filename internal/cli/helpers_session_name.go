package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

var errMissingSessionName = errors.New("missing --session (or set CDP_SESSION_NAME/WEB_SESSION/WEB_SESSION_ID)")

// addSessionFlag adds the standard --session flag used by commands that operate on
// a saved CDP session.
func addSessionFlag(fs *flag.FlagSet) *string {
	return fs.String("session", "", "Session name (or set CDP_SESSION_NAME/WEB_SESSION/WEB_SESSION_ID)")
}

func resolveSessionName(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	for _, k := range []string{"CDP_SESSION_NAME", "WEB_SESSION", "WEB_SESSION_ID"} {
		if v := os.Getenv(k); v != "" {
			return v, nil
		}
	}
	return "", errMissingSessionName
}

func unexpectedArgs(pos []string) error {
	if len(pos) == 0 {
		return nil
	}
	return fmt.Errorf("unexpected argument: %s", pos[0])
}

