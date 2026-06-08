package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/pkg/cli"
	"github.com/homegrew/grew/pkg/cmd"
	verpkg "github.com/homegrew/grew/pkg/version"
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
		slog.Debug("No command provided, exiting", "exe", os.Args[0])
		os.Exit(1)
	}
	slog.Debug("Starting test binary", "exe", os.Args[0], "args", os.Args)

	cli.SetupTestEnvironment()

	slog.Debug("Starting test binary", "exe", os.Args[0], "args", os.Args)
	switch os.Args[1] {
	case "run":
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := cmd.ExportSelfUpdateFromRelease(exePath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "from-release":
		exePath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := cmd.ExportSelfUpdateFromRelease(exePath); err != nil {
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
