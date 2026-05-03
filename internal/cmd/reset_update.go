package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/tap"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

func canonicalPath(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		if abs, err := filepath.Abs(resolved); err == nil {
			return filepath.Clean(abs), nil
		}
		return filepath.Clean(resolved), nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

var ResetUpdateCmd = &cobra.Command{
	Use:   "reset-update",
	Short: "Wipe and re-fetch all tap definitions",
	Long: `Delete all tap definitions and re-fetch them from scratch. Use this when
'grew update' fails or tap data is corrupted.

What it does:
  1. Removes the entire Taps directory
  2. Re-creates the directory structure
  3. Fetches fresh tap definitions (via API or git clone)

Installed packages in the Cellar are NOT affected.

Examples:
  grew reset-update`,
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Debug("starting resetupdate command execution")
		paths := config.Default()

		if !paths.IsUnderRoot(paths.Taps) || paths.Taps == paths.Root {
			return fmt.Errorf("refusing to remove taps outside root: root=%q taps=%q", paths.Root, paths.Taps)
		}

		ui.FprintArrow(os.Stderr, "Removing taps directory %s", paths.Taps)
		if err := os.RemoveAll(paths.Taps); err != nil {
			return fmt.Errorf("remove taps: %w", err)
		}

		if err := paths.Init(); err != nil {
			return err
		}

		// Remove unsupported share directories if they exist from a prior run
		if rootCanonical, err := canonicalPath(paths.Root); err == nil {
			sharePath := filepath.Join(paths.Root, "share")
			manPath := filepath.Join(sharePath, "man")
			infoPath := filepath.Join(sharePath, "info")

			shareCanonical, shareErr := canonicalPath(paths.Share)
			manCanonical, manErr := canonicalPath(manPath)
			infoCanonical, infoErr := canonicalPath(infoPath)
			expectedShareCanonical, expectedShareErr := canonicalPath(sharePath)

			if shareErr == nil && expectedShareErr == nil &&
				manErr == nil && infoErr == nil &&
				shareCanonical == expectedShareCanonical &&
				shareCanonical != rootCanonical {
				_ = os.RemoveAll(manCanonical)
				_ = os.RemoveAll(infoCanonical)
				_ = os.Remove(shareCanonical) // only removes if empty
			}
		}

		tapMgr := &tap.Manager{TapsDir: paths.Taps}
		tapsCount, formulaCount, err := tapMgr.Update()
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}

		ui.FprintArrow(os.Stderr, "Tap definitions reset and updated (%d taps, %d formulas found)", tapsCount, formulaCount)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(ResetUpdateCmd)
}
