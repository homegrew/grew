package cmd

import (
	"flag"
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/linker"
)

func runLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ContinueOnError)
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

	ctx, err := newCommonCtx()
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
			fmt.Printf("==> %s %s linked\n", name, ver)
		}
	}
	return nil
}

func runUnlink(args []string) error {
	fs := flag.NewFlagSet("unlink", flag.ContinueOnError)
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
			fmt.Printf("==> %s unlinked\n", name)
		}
	}
	return nil
}
