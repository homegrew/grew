// Command grew is the entry point for the grew package manager.
// It delegates command execution to the internal/cmd package.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/homegrew/grew/internal/cmd"
	verpkg "github.com/homegrew/grew/internal/version"
)

func init() {
	verpkg.SetVersion(version)
}

var version string

// Version returns the version string.
func Version() string {
	return strings.TrimSpace(version)
}

func main() {
	if err := cmd.Run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "grew: %s\n", err)
		os.Exit(1)
	}
}
