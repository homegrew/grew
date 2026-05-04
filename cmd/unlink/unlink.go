package unlink

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/linker"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

var (
	unlinkDryRun  bool
)

var Command = &cobra.Command{
	Use:   "unlink [flags] <formula ...>",
	Short: "Remove symlinks for formulas",
	Long: `Remove symlinks for an installed formula without uninstalling it.

Examples:
  grew unlink jq`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUnlink(args)
	},
}

func init() {
	Command.Flags().BoolVarP(&unlinkDryRun, "dry-run", "n", false, "Show what would be unlinked without making changes")
}

func runUnlink(args []string) error {
	slog.Debug("starting unlink command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew unlink [--dry-run] <formula>...")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}

	for _, name := range args {
		if !cel.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		if err := lnk.UnlinkWithOpts(name, linker.UnlinkOpts{DryRun: unlinkDryRun}); err != nil {
			return err
		}

		if unlinkDryRun {
			slog.Info("(dry run, no changes made)")
		} else {
			slog.Info("removed symlinks from bin/, lib/, include/, opt/")
			ui.FprintArrow(os.Stderr, "%s unlinked", name)
		}
	}
	return nil
}
