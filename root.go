package main

import (
	"github.com/homegrew/grew/pkg/cli"
	intcmd "github.com/homegrew/grew/pkg/cmd"
	intver "github.com/homegrew/grew/pkg/version"
	"github.com/spf13/cobra"
)

var Grew = &cobra.Command{
	Use:     "grew",
	Short:   "A package manager written in Go",
	Long:    `grew is a high-performance, Go-based command-line package manager inspired by Homebrew.`,
	Version: intver.Version(),
}

func init() {
	cli.SetupTestEnvironment()
	cli.InitializeRootCommand(Grew)
	cli.AddCommands(Grew)

	// Add legacy commands from pkg/cmd
	intcmd.AddLegacyCommands(Grew)
}

// Execute executes the root command.
func Execute(args []string) error {
	Grew.Version = intver.Version()
	Grew.SetArgs(args)
	return Grew.Execute()
}
