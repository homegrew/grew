package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

var (
	uninstallCask  bool
	uninstallForce bool
)

var UninstallCmd = &cobra.Command{
	Use:     "uninstall [flags] <formula>",
	Aliases: []string{"remove", "rm"},
	Short:   "Uninstall formulas or casks",
	Long: `Uninstall a formula by removing its symlinks and Cellar directory.
With --cask, removes the .app from ~/Applications and the Caskroom entry.

Examples:
  grew uninstall jq
  grew uninstall --force jq
  grew uninstall --cask firefox`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUninstall(args)
	},
}

func init() {
	UninstallCmd.Flags().BoolVar(&uninstallCask, "cask", false, "Uninstall a cask instead of a formula.")
	UninstallCmd.Flags().BoolVarP(&uninstallForce, "force", "f", false, "Delete all installed versions of formula. Uninstall even if cask is not installed.")
	rootCmd.AddCommand(UninstallCmd)
}

func runUninstall(args []string) error {
	slog.Debug("starting uninstall command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew uninstall [-f] [--cask] <formula>...")
	}

	if uninstallCask {
		for _, name := range args {
			if err := caskUninstall(name, uninstallForce); err != nil {
				return err
			}
		}
		return nil
	}

	ctx, err := newInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	for _, name := range args {
		if err := uninstallFormula(ctx, name, uninstallForce); err != nil {
			return err
		}
	}

	return nil
}

func uninstallFormula(ctx *installContext, name string, force bool) error {
	if !ctx.Cellar.IsInstalled(name) {
		if !force {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
		}
		return nil
	}

	ver, _ := ctx.Cellar.InstalledVersion(name)
	kegPath, _ := ctx.Cellar.KegPath(name, ver)
	slog.Info("cellar path: " + kegPath)

	ui.FprintArrow(os.Stderr, "Unlinking %s...", name)
	ctx.Linker.Unlink(name)
	slog.Info("removed symlinks from bin/, lib/, include/, opt/")

	ui.FprintArrow(os.Stderr, "Removing %s...", name)
	if err := ctx.Cellar.Uninstall(name); err != nil {
		if force {
			slog.Warn(fmt.Sprintf("ignoring error while removing %s: %v", name, err))
		} else {
			return err
		}
	}

	ctx.AuditLog.Log(auditlog.ActionUninstall, name, ver, "", "")
	ui.FprintArrow(os.Stderr, "%s uninstalled", name)
	return nil
}
