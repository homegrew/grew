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
	fs := flag.NewFlagSet("reinstall", flag.ContinueOnError)
	flags.Register(fs)
	force := fs.Bool("force", false, "Install even if not currently installed")
	fs.BoolVar(force, "f", false, "Install even if not currently installed")
	zap := fs.Bool("zap", false, "Remove all versions and temp files before reinstalling")
	buildFromSource := fs.Bool("s", false, "Build from source")
	fs.BoolVar(buildFromSource, "build-from-source", false, "Build from source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if fs.NArg() != 1 {
		return fmt.Errorf("usage: grew reinstall [-f] [--zap] [-s] <formula>")
	}
	name := fs.Arg(0)

	ctx, err := newInstallContext()
	if err != nil {
		return err
	}

	if !ctx.Cellar.IsInstalled(name) && !*force {
		return fmt.Errorf("formula %q is not installed (use --force to install anyway)", name)
	}

	f, err := ctx.Loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	fmt.Printf("==> Reinstalling %s %s\n", f.Name, f.Version)

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
		return installFormulaFromSource(f, ctx, opts)
	}
	return installFormula(f, ctx, opts)
}

// cleanTmpFor removes staging and build dirs for a formula from the tmp directory.
func cleanTmpFor(rootDir, tmpDir, name string) {
	baseRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return
	}
	baseRootDir = filepath.Clean(baseRootDir)
	if err := safepath.SafeAbsolutePath(baseRootDir); err != nil {
		return
	}

	baseTmpDir, err := filepath.Abs(tmpDir)
	if err != nil {
		return
	}
	baseTmpDir = filepath.Clean(baseTmpDir)
	if err := safepath.SafeAbsolutePath(baseTmpDir); err != nil {
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
