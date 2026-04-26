package cmd

import (
	"flag"
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/internal/linker"
)

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew uninstall [options] <formula ...>

Removes installed formulas or casks from the cellar and removes their symlinks.
Aliases: remove, rm

Options:
  --cask        Uninstall a cask instead of a formula.
  --force, -f   Delete all installed versions of formula. Uninstall even if
                cask is not installed, overwrite existing files and ignore
                errors when removing files.
  -v, --verbose Show detailed output.
  -d, --debug   Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	isCask := fs.Bool("cask", false, "Uninstall a cask")
	forceDesc := "Delete all installed versions of formula. Uninstall even if cask is not installed, overwrite existing files and ignore errors when removing files."
	force := fs.Bool("force", false, forceDesc)
	fs.BoolVar(force, "f", false, forceDesc)
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if fs.NArg() == 0 {
		return fmt.Errorf("usage: grew uninstall [-f] [--cask] <formula>...")
	}

	if *isCask {
		for _, name := range fs.Args() {
			if err := caskUninstall(name, *force); err != nil {
				return err
			}
		}
		return nil
	}

	paths := config.Default()

	lock, err := acquireGlobalLock(paths)
	if err != nil {
		return err
	}
	defer func() {
		fsutil.Unlock(lock)
		if err := lock.Close(); err != nil {
			slog.Error("failed to close global lock file", "err", err)
		}
	}()

	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}
	auditLogger := auditlog.New(paths.Log)

	for _, name := range fs.Args() {
		if !cel.IsInstalled(name) {
			if !*force {
				slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			}
			continue
		}

		ver, _ := cel.InstalledVersion(name)
		kegPath, _ := cel.KegPath(name, ver)
		slog.Info("cellar path: " + kegPath)

		fmt.Printf("==> Unlinking %s...\n", name)
		lnk.Unlink(name)
		slog.Info("removed symlinks from bin/, lib/, include/, opt/")

		fmt.Printf("==> Removing %s...\n", name)
		if err := cel.Uninstall(name); err != nil {
			if *force {
				slog.Warn(fmt.Sprintf("ignoring error while removing %s: %v", name, err))
			} else {
				return err
			}
		}

		auditLogger.Log(auditlog.ActionUninstall, name, ver, "", "")
		fmt.Printf("==> %s uninstalled\n", name)
	}

	return nil
}
