package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/linker"
)

func runLink(args []string) error {
	slog.Debug("starting link command execution")
	slog.Debug("starting link command execution")
	fs := flag.NewFlagSet("link", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew link [options] <formula ...>

Create symlinks for installed formulas in the grew prefix.

Options:
  --overwrite   Overwrite existing files when linking.
  -n, --dry-run Show what would be linked, but do not actually link anything.
  --force       Link keg-only formulas into bin/, lib/, and include/.
  -v, --verbose Show detailed output.
  -d, --debug   Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	overwrite := fs.Bool("overwrite", false, "Overwrite existing files")
	dryRun := fs.Bool("dry-run", false, "Show what would be linked")
	fs.BoolVar(dryRun, "n", false, "Show what would be linked")
	force := fs.Bool("force", false, "Link keg-only formula into bin/, lib/, include/")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if fs.NArg() == 0 {
		return fmt.Errorf("usage: grew link [--overwrite] [--dry-run] [--force] <formula>...")
	}

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	for _, name := range fs.Args() {
		if !ctx.Cellar.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		ver, err := ctx.Cellar.InstalledVersion(name)
		if err != nil {
			return err
		}

		f, err := ctx.Loader.LoadByName(name)
		kegOnly := false
		if err == nil {
			kegOnly = f.KegOnly
		}

		if kegOnly && !*force {
			fmt.Printf("Warning: %s is keg-only. Use --force to link anyway.\n", name)
		}

		lnk := &linker.Linker{Paths: ctx.Paths}
		kegPath, _ := ctx.Cellar.KegPath(name, ver)
		slog.Info("keg: " + kegPath)
		opts := linker.LinkOpts{
			KegOnly:   kegOnly,
			Overwrite: *overwrite,
			DryRun:    *dryRun,
			Force:     *force,
		}
		if err := lnk.LinkWithOpts(name, ver, opts); err != nil {
			return err
		}
		slog.Info(fmt.Sprintf("opt/%s -> %s", name, kegPath))
		if !kegOnly || *force {
			slog.Info("symlinked bin/, lib/, include/ contents")
		}

		if !*dryRun {
			fmt.Fprintf(os.Stderr, "==> %s %s linked\n", name, ver)
		}
	}
	return nil
}

func runUnlink(args []string) error {
	slog.Debug("starting unlink command execution")
	slog.Debug("starting unlink command execution")
	fs := flag.NewFlagSet("unlink", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew unlink [options] <formula ...>

Remove symlinks for installed formulas from the grew prefix.

Options:
  -n, --dry-run Show what would be unlinked, but do not actually unlink anything.
  -v, --verbose Show detailed output.
  -d, --debug   Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	dryRun := fs.Bool("dry-run", false, "Show what would be unlinked")
	fs.BoolVar(dryRun, "n", false, "Show what would be unlinked")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if fs.NArg() == 0 {
		return fmt.Errorf("usage: grew unlink [--dry-run] <formula>...")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}

	for _, name := range fs.Args() {
		if !cel.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		if err := lnk.UnlinkWithOpts(name, linker.UnlinkOpts{DryRun: *dryRun}); err != nil {
			return err
		}

		if *dryRun {
			slog.Info("(dry run, no changes made)")
		} else {
			slog.Info("removed symlinks from bin/, lib/, include/, opt/")
			fmt.Fprintf(os.Stderr, "==> %s unlinked\n", name)
		}
	}
	return nil
}
