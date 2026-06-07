package resetupdate

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/runtime"
	"github.com/homegrew/grew/pkg/tap"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/spf13/cobra"
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

func isWithinBase(base, target string) bool {
	baseCanonical, err := canonicalPath(base)
	if err != nil {
		return false
	}
	targetCanonical, err := canonicalPath(target)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(baseCanonical, targetCanonical)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func isExactCanonicalChild(rootCanonical, targetCanonical string, elems ...string) bool {
	expectedCanonical, err := canonicalPath(filepath.Join(append([]string{rootCanonical}, elems...)...))
	if err != nil {
		return false
	}
	return targetCanonical == expectedCanonical && isWithinBase(rootCanonical, targetCanonical)
}

var Command = &cobra.Command{
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
		ctx, err := context.NewInstallContext()
		if err != nil {
			return err
		}
		defer ctx.Close()
		paths := ctx.Paths

		// Extra safety: only allow destructive operations under the trusted default prefix.
		trustedRoot := config.DefaultPrefix()
		trustedRootCanonical, trustedRootErr := canonicalPath(trustedRoot)
		pathsRootCanonical, pathsRootErr := canonicalPath(paths.Root)
		if trustedRootErr != nil || pathsRootErr != nil || trustedRootCanonical != pathsRootCanonical {
			return fmt.Errorf("refusing to remove paths for untrusted root: root=%q trusted=%q", paths.Root, trustedRoot)
		}

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

		// Remove unsupported share directories if they exist from a prior run.
		// Use only the system prefix as trusted base to avoid user-controlled path influence.
		if trustedSystemRoot, trustedSystemErr := canonicalPath(runtime.SystemPrefix()); trustedSystemErr == nil {
			if rootCanonical, err := canonicalPath(paths.Root); err == nil && rootCanonical == trustedSystemRoot {
				sharePath := filepath.Join(trustedSystemRoot, "share")
				manPath := filepath.Join(sharePath, "man")
				infoPath := filepath.Join(sharePath, "info")

				shareCanonical, shareErr := canonicalPath(sharePath)
				manCanonical, manErr := canonicalPath(manPath)
				infoCanonical, infoErr := canonicalPath(infoPath)

				if shareErr == nil &&
					manErr == nil && infoErr == nil &&
					shareCanonical != trustedSystemRoot &&
					isWithinBase(trustedSystemRoot, shareCanonical) &&
					isWithinBase(trustedSystemRoot, manCanonical) &&
					isWithinBase(trustedSystemRoot, infoCanonical) {
					trustedShareCanonical, trustedShareErr := canonicalPath(filepath.Join(trustedSystemRoot, "share"))
					trustedManCanonical, trustedManErr := canonicalPath(filepath.Join(trustedSystemRoot, "share", "man"))
					trustedInfoCanonical, trustedInfoErr := canonicalPath(filepath.Join(trustedSystemRoot, "share", "info"))
					if trustedShareErr == nil && trustedManErr == nil && trustedInfoErr == nil &&
						trustedShareCanonical != trustedSystemRoot &&
						isExactCanonicalChild(trustedSystemRoot, trustedShareCanonical, "share") &&
						isExactCanonicalChild(trustedSystemRoot, trustedManCanonical, "share", "man") &&
						isExactCanonicalChild(trustedSystemRoot, trustedInfoCanonical, "share", "info") {
						_ = os.RemoveAll(trustedManCanonical)
						_ = os.RemoveAll(trustedInfoCanonical)
						_ = os.Remove(trustedShareCanonical) // only removes if empty
					}
				}
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
}
