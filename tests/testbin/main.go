package main

import (
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cli"
	"github.com/homegrew/grew/internal/cmd"
	"github.com/homegrew/grew/internal/installer"
	verpkg "github.com/homegrew/grew/internal/version"
	"github.com/spf13/cobra"
)

var buildVersion string

func init() {
	if buildVersion != "" {
		verpkg.SetVersion(buildVersion)
	}
}

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}

	cli.SetupTestEnvironment()

	switch os.Args[1] {
	case "run":
		if err := installer.RunSelfUpdate(nil); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "from-release":
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := installer.SelfUpdateFromRelease(exePath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		// Delegate everything else (like "install", "_extract") to the real command router
		testCmd := &cobra.Command{Use: "grew"}
		testCmd.Version = verpkg.Version()
		cli.InitializeRootCommand(testCmd)
		cli.AddCommands(testCmd)
		cmd.AddLegacyCommands(testCmd)

		testCmd.SetArgs(os.Args[1:])
		if err := testCmd.Execute(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}
