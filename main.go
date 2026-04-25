// Command grew is the entry point for the grew package manager.
// It delegates command execution to the internal/cmd package.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cmd"
)

func main() {
	if err := cmd.Run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "grew: %s\n", err)
		os.Exit(1)
	}
}
