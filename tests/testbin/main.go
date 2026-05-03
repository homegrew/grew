package main

import (
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cmd"
	verpkg "github.com/homegrew/grew/internal/version"
)

var version string

func init() {
	verpkg.SetVersion(version)
}

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if err := cmd.RunSelfUpdate(nil); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "from-release":
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := cmd.SelfUpdateFromRelease(exePath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		// Delegate everything else (like "install", "_extract") to the real command router
		fmt.Fprintf(os.Stderr, "Usage: grew run [test binary]: %s\n\n", os.Args)
		if err := cmd.Run(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
