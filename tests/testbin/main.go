package main

import (
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cmd"
	verpkg "github.com/homegrew/grew/internal/version"
	"github.com/spf13/cobra"
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
		// We'll create a dummy root command to route
		testCmd := &cobra.Command{Use: "test"}
		cmd.AddLegacyCommands(testCmd)
		testCmd.SetArgs(os.Args[1:])
		if err := testCmd.Execute(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
