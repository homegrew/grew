// Command grew is the entry point for the grew package manager.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	verpkg "github.com/homegrew/grew/internal/version"
)

var buildVersion string

func init() {
	if buildVersion != "" {
		verpkg.SetVersion(buildVersion)
	}
}

func main() {
	Grew.Version = verpkg.Version()
	Grew.SetArgs(os.Args[1:])
	if err := Grew.Execute(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "grew: %s\n", err)
		os.Exit(1)
	}
}
