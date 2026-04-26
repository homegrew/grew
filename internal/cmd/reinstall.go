package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/pkg/safepath"
)

func runReinstall(args []string) error {
	slog.Debug("starting reinstall command execution")
	slog.Debug("starting reinstall command execution")
	fs := flag.NewFlagSet("reinstall", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew reinstall [options] <formula ...>

Uninstall and then reinstall formulas or casks from scratch.

Options:
  --cask              Reinstall a cask instead of a formula.
  -f, --force         Install without checking for previously installed keg-only or
                      non-migrated versions.
  --zap               Deep clean: remove all installed versions and any leftover
                      temp files for the formula before reinstalling.
  -s, --build-from-source
                      Build formula from source instead of downloading a bottle.
  -v, --verbose       Show detailed output.
  -d, --debug         Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	isCask := fs.Bool("cask", false, "Reinstall a cask")
	force := fs.Bool("force", false, "Install without checking for previously installed keg-only or non-migrated versions")
	fs.BoolVar(force, "f", false, "Install without checking for previously installed keg-only or non-migrated versions")
	zap := fs.Bool("zap", false, "Remove all versions and temp files before reinstalling")
	buildFromSource := fs.Bool("s", false, "Build from source")
	fs.BoolVar(buildFromSource, "build-from-source", false, "Build from source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if fs.NArg() == 0 {
		return fmt.Errorf("usage: grew reinstall [--cask] [-f] [--zap] [-s] <formula>...")
	}

	if *isCask {
		if *buildFromSource {
			return fmt.Errorf("--build-from-source is not supported for casks")
		}
		if *zap {
			return fmt.Errorf("--zap is not currently supported for casks")
		}
		for _, name := range fs.Args() {
			_, _, cr, err := setupCaskLoader()
			if err != nil {
				return err
			}

			if !cr.IsInstalled(name) && !*force {
				return fmt.Errorf("cask %q is not installed (use --force to install anyway)", name)
			}

			fmt.Fprintf(os.Stderr, "==> Reinstalling cask %s\n", name)

			if cr.IsInstalled(name) {
				if err := caskUninstall(name, true); err != nil {
					return fmt.Errorf("remove old installation: %w", err)
				}
			}

			// noQuarantine defaults to false here as per install behavior
			if err := caskInstall(name, false, true); err != nil {
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

	for _, name := range fs.Args() {
		if !ctx.Cellar.IsInstalled(name) && !*force {
			return fmt.Errorf("formula %q is not installed (use --force to install anyway)", name)
		}

		f, err := ctx.Loader.LoadByName(name)
		if err != nil {
			return fmt.Errorf("formula not found: %s", name)
		}

		fmt.Fprintf(os.Stderr, "==> Reinstalling %s %s\n", f.Name, f.Version)

		// Unlink existing installation.
		if ctx.Cellar.IsInstalled(name) {
			ctx.Linker.Unlink(name)
			slog.Info("unlinked " + name)
		}

		if *zap {
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
		opts := installOpts{installedOnRequest: true}
		if *buildFromSource {
			if err := installFormulaFromSource(f, ctx, opts); err != nil {
				return err
			}
		} else {
			if err := installFormula(f, ctx, opts); err != nil {
				return err
			}
		}
	}

	return nil
}

// cleanTmpFor removes staging and build dirs for a formula from the tmp directory.
func cleanTmpFor(rootDir, tmpDir, name string) {
	baseRootDir, err := normalizeDir(rootDir, "root")
	if err != nil {
		return
	}

	baseTmpDir, err := normalizeDir(tmpDir, "tmp")
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
