package uninstall

import (
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/cmd"
	"github.com/spf13/cobra"
)

var (
	uninstallCask  bool
	uninstallForce bool
)

var Command = &cobra.Command{
	Use:     "uninstall [flags] <formula>",
	Aliases: []string{"remove", "rm"},
	Short:   "Uninstall formulas or casks",
	Long: `Uninstall a formula by removing its symlinks and Cellar directory.
With --cask, removes the .app from ~/Applications and the Caskroom entry.

Examples:
  grew uninstall jq
  grew uninstall --force jq
  grew uninstall --cask firefox`,
	RunE: func(c *cobra.Command, args []string) error {
		return runUninstall(args)
	},
}

func init() {
	Command.Flags().BoolVar(&uninstallCask, "cask", false, "Uninstall a cask instead of a formula.")
	Command.Flags().BoolVarP(&uninstallForce, "force", "f", false, "Delete all installed versions of formula. Uninstall even if cask is not installed.")
}

func runUninstall(args []string) error {
	slog.Debug("starting uninstall command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew uninstall [-f] [--cask] <formula>...")
	}

	if uninstallCask {
		for _, name := range args {
			if err := cmd.CaskUninstall(name, uninstallForce); err != nil {
				return err
			}
		}
		return nil
	}

	ctx, err := cmd.NewInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	for _, name := range args {
		if err := cmd.UninstallFormula(ctx, name, uninstallForce); err != nil {
			return err
		}
	}

	return nil
}
