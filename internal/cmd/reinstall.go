package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func runReinstall(args []string) error {
	fs := flag.NewFlagSet("reinstall", flag.ContinueOnError)
	force := fs.Bool("force", false, "Install even if not currently installed")
	fs.BoolVar(force, "f", false, "Install even if not currently installed")
	zap := fs.Bool("zap", false, "Remove all versions and temp files before reinstalling")
	buildFromSource := fs.Bool("s", false, "Build from source")
	fs.BoolVar(buildFromSource, "build-from-source", false, "Build from source")
	if err := fs.Parse(args); err != nil {
		return err
	}

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
		for _, ver := range versions {
			kegPath, _ := ctx.Cellar.KegPath(name, ver)
			slog.Info(fmt.Sprintf("removing %s %s", name, ver))
			os.RemoveAll(kegPath)
		}
		// Remove any leftover staging/build dirs in tmp.
		cleanTmpFor(ctx.Paths.Tmp, name)
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
func cleanTmpFor(tmpDir, name string) {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		// Match patterns like "jq-1.0-stage", "jq-1.0-build".
		if matched, _ := filepath.Match(name+"-*", e.Name()); matched {
			os.RemoveAll(filepath.Join(tmpDir, e.Name()))
		}
	}
}
