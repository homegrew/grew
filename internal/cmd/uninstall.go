package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/flags"
)

func runUninstall(args []string) error {
	slog.Debug("starting uninstall command execution")
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

	ctx, err := newInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	for _, name := range fs.Args() {
		if err := uninstallFormula(ctx, name, *force); err != nil {
			return err
		}
	}

	return nil
}

func uninstallFormula(ctx *installContext, name string, force bool) error {
	if !ctx.Cellar.IsInstalled(name) {
		if !force {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
		}
		return nil
	}

	ver, _ := ctx.Cellar.InstalledVersion(name)
	kegPath, _ := ctx.Cellar.KegPath(name, ver)
	slog.Info("cellar path: " + kegPath)

	fmt.Fprintf(os.Stderr, "==> Unlinking %s...\n", name)
	ctx.Linker.Unlink(name)
	slog.Info("removed symlinks from bin/, lib/, include/, opt/")

	fmt.Fprintf(os.Stderr, "==> Removing %s...\n", name)
	if err := ctx.Cellar.Uninstall(name); err != nil {
		if force {
			slog.Warn(fmt.Sprintf("ignoring error while removing %s: %v", name, err))
		} else {
			return err
		}
	}

	ctx.AuditLog.Log(auditlog.ActionUninstall, name, ver, "", "")
	fmt.Fprintf(os.Stderr, "==> %s uninstalled\n", name)
	return nil
}
