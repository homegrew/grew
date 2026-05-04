package reinstall

import (
	"github.com/homegrew/grew/internal/cmd"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/pkg/safepath"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

var (
	reinstallCask            bool
	reinstallForce           bool
	reinstallZap             bool
	reinstallBuildFromSource bool
)

var Command = &cobra.Command{
	Use:   "reinstall [flags] <formula>",
	Short: "Reinstall formulas or casks",
	Long: `Uninstall and then reinstall a formula or cask. This is useful when an
installation is corrupted or you want a clean slate.

Examples:
  grew reinstall jq
  grew reinstall --cask firefox
  grew reinstall -f jq`,
	RunE: func(c *cobra.Command, args []string) error {
		return runReinstall(args)
	},
}

func init() {
	Command.Flags().BoolVar(&reinstallCask, "cask", false, "Reinstall a cask instead of a formula")
	Command.Flags().BoolVarP(&reinstallForce, "force", "f", false, "Install without checking for previously installed keg-only or non-migrated versions.")
	Command.Flags().BoolVar(&reinstallZap, "zap", false, "Deep clean: remove all installed versions and any leftover temp files before reinstalling.")
	Command.Flags().BoolVarP(&reinstallBuildFromSource, "build-from-source", "s", false, "Build formula from source instead of downloading a bottle.")
}

func runReinstall(args []string) error {
	slog.Debug("starting reinstall command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew reinstall [--cask] [-f] [--zap] [-s] <formula>...")
	}

	if reinstallCask {
		if reinstallBuildFromSource {
			return fmt.Errorf("--build-from-source is not supported for casks")
		}
		if reinstallZap {
			return fmt.Errorf("--zap is not currently supported for casks")
		}
		for _, name := range args {
			_, _, cr, err := cmd.SetupCaskLoader()
			if err != nil {
				return err
			}

			if !cr.IsInstalled(name) && !reinstallForce {
				return fmt.Errorf("cask %q is not installed (use --force to install anyway)", name)
			}

			ui.FprintArrow(os.Stderr, "Reinstalling cask %s", name)

			if cr.IsInstalled(name) {
				if err := cmd.CaskUninstall(name, true); err != nil {
					return fmt.Errorf("remove old installation: %w", err)
				}
			}

			// noQuarantine defaults to false here as per install behavior
			if err := cmd.CaskInstall(name, false, true); err != nil {
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
		if !ctx.Cellar.IsInstalled(name) && !reinstallForce {
			return fmt.Errorf("formula %q is not installed (use --force to install anyway)", name)
		}

		f, err := ctx.Loader.LoadByName(name)
		if err != nil {
			return fmt.Errorf("formula not found: %s", name)
		}

		ui.FprintArrow(os.Stderr, "Reinstalling %s %s", f.Name, f.Version)

		// Unlink existing installation.
		if ctx.Cellar.IsInstalled(name) {
			ctx.Linker.Unlink(name)
			slog.Info("unlinked " + name)
		}

		if reinstallZap {
			// Remove all installed versions of this formula.
			versions, _ := ctx.Cellar.InstalledVersions(name)

			resolvedCellarBase, err := filepath.EvalSymlinks(ctx.Cellar.Path)
			if err != nil {
				if resolvedCellarBase, err = filepath.Abs(ctx.Cellar.Path); err != nil {
					return fmt.Errorf("resolve cellar path: %w", err)
				}
			}
			resolvedCellarBase = filepath.Clean(resolvedCellarBase)

			for _, ver := range versions {
				kegPath, err := ctx.Cellar.KegPath(name, ver)
				if err != nil {
					slog.Warn(fmt.Sprintf("skipping invalid keg path for %s %s: %v", name, ver, err))
					continue
				}

				resolvedKegPath, err := filepath.EvalSymlinks(kegPath)
				if err != nil {
					if resolvedKegPath, err = filepath.Abs(kegPath); err != nil {
						slog.Warn(fmt.Sprintf("skipping unresolved keg path for %s %s: %v", name, ver, err))
						continue
					}
				}
				resolvedKegPath = filepath.Clean(resolvedKegPath)

				if err := safepath.CheckSubpath(resolvedCellarBase, resolvedKegPath); err != nil {
					slog.Warn(fmt.Sprintf("skipping unsafe keg path for %s %s: %v", name, ver, err))
					continue
				}
				slog.Info(fmt.Sprintf("removing %s %s", name, ver))
				if err := os.RemoveAll(resolvedKegPath); err != nil {
					slog.Warn(fmt.Sprintf("failed removing %s: %v", resolvedKegPath, err))
				}
			}
			// Remove any leftover staging/build dirs in tmp.
			cleanTmpFor(ctx.Paths.Root, ctx.Paths.Tmp, name)
			slog.Info("zapped all versions and temp files for " + name)
		} else if ctx.Cellar.IsInstalled(name) {
			if err := ctx.Cellar.Uninstall(name); err != nil {
				return fmt.Errorf("remove old installation: %w", err)
			}
			slog.Info("removed old cellar entry")
		}

		// Fresh install.
		opts := cmd.InstallOpts{InstalledOnRequest: true}
		if reinstallBuildFromSource {
			if err := cmd.InstallFormulaFromSource(f, ctx, opts); err != nil {
				return err
			}
		} else {
			if err := cmd.InstallFormula(f, ctx, opts); err != nil {
				return err
			}
		}
	}

	return nil
}

// cleanTmpFor removes staging and build dirs for a formula from the tmp directory.
func cleanTmpFor(rootDir, tmpDir, name string) {
	baseRootDir, err := cmd.NormalizeDir(rootDir, "root")
	if err != nil {
		return
	}

	baseTmpDir, err := cmd.NormalizeDir(tmpDir, "tmp")
	if err != nil {
		return
	}
	if err := safepath.CheckSubpath(baseRootDir, baseTmpDir); err != nil {
		return
	}

	entries, err := os.ReadDir(baseTmpDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// Match patterns like "jq-1.0-stage", "jq-1.0-build".
		if matched, _ := filepath.Match(name+"-*", e.Name()); matched {
			target := filepath.Join(baseTmpDir, e.Name())
			if err := safepath.CheckSubpath(baseTmpDir, target); err != nil {
				continue
			}
			os.RemoveAll(target)
		}
	}
}
