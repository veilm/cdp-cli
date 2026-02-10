package main

import (
	"fmt"
	"os"

	"github.com/veilm/cdp-cli/internal/cli"
)

func main() {
	if err := cli.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
