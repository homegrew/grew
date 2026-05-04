package unpin

import (
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "unpin <formula>...",
	Short: "Unpin formulas to allow upgrades",
	Long:  "Unpin formulas to allow them to be upgraded automatically.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUnpin(args)
	},
}

func runUnpin(args []string) error {
	slog.Debug("starting unpin command execution")
	
	if len(args) == 0 {
		return fmt.Errorf("usage: grew unpin <formula>...")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}
	logger := auditlog.New(paths.Log)

	for _, name := range args {
		if !cel.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		if !cel.IsPinned(name) {
			fmt.Printf("%s is not pinned.\n", name)
			continue
		}

		if err := cel.Unpin(name); err != nil {
			return err
		}
		logger.Log(auditlog.ActionUnpin, name, "", "", "")
		fmt.Printf("Unpinned %s.\n", name)
	}

	return nil
}
