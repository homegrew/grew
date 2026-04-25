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
	flags.Register(fs)
	isCask := fs.Bool("cask", false, "Uninstall a cask")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if fs.NArg() != 1 {
		return fmt.Errorf("usage: grew uninstall [--cask] <formula>")
	}

	if *isCask {
		return caskUninstall(fs.Arg(0))
	}

	name := fs.Arg(0)
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

	if !cel.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed", name)
	}

	lnk := &linker.Linker{Paths: paths}

	ver, _ := cel.InstalledVersion(name)
	kegPath, _ := cel.KegPath(name, ver)
	slog.Info("cellar path: " + kegPath)

	fmt.Printf("==> Unlinking %s...\n", name)
	lnk.Unlink(name)
	slog.Info("removed symlinks from bin/, lib/, include/, opt/")

	fmt.Printf("==> Removing %s...\n", name)
	if err := cel.Uninstall(name); err != nil {
		return err
	}

	auditlog.New(paths.Log).Log(auditlog.ActionUninstall, name, ver, "", "")

	fmt.Printf("==> %s uninstalled\n", name)
	return nil
}
