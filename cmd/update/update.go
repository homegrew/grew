package update

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cmd"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/runtime"
	"github.com/homegrew/grew/internal/tap"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

var Command = &cobra.Command{
    Use:     "update",
    Aliases: []string{"up"},
    Short:   "fetch the newest version of grew and all formulae",
    Long: `Fetch the newest version of grew and all formulae from GitHub
using git(1). Equivalent to: git -C <taps-dir> pull

The taps repository is cloned from:
  https://github.com/homegrew/homegrew-taps`,
    RunE: func(c *cobra.Command, args []string) error {
        return runUpdate(args)
    },
}

func runUpdate(args []string) error {
    slog.Debug("starting update command execution")
    // Attempt to self-update the CLI binary first, unless we are in devmode.
    if !runtime.DevMode {
        fmt.Fprintln(os.Stderr, "==> Checking for grew updates...")
        if err := cmd.RunSelfUpdate(nil); err != nil {
            slog.Warn("self-update failed, continuing with tap update", "error", err)
        }
    } else {
        slog.Info("skipping self-update in devmode build")
    }

    // Update tap definitions.
    paths := config.Default()
    if err := paths.Init(); err != nil {
        return err
    }

    tapMgr := &tap.Manager{TapsDir: paths.Taps}
    tapsCount, formulaCount, err := tapMgr.Update()
    if err != nil {
        auditlog.New(paths.Log).Log(auditlog.ActionUpdate, "all", "", "", fmt.Sprintf("failed: %v", err))
        return fmt.Errorf("update taps: %w", err)
    }

    if formulaCount == 0 {
        auditlog.New(paths.Log).Log(auditlog.ActionUpdate, "all", "", "", "already up-to-date")
    } else {
        auditlog.New(paths.Log).Log(auditlog.ActionUpdate, "all", "", "", fmt.Sprintf("updated %d taps, %d formulas found", tapsCount, formulaCount))
    }

    ui.FprintArrow(os.Stderr, "Updated %d taps (%d formulas found)", tapsCount, formulaCount)
    return nil
}