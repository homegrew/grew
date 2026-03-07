package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
)

func runLink(args []string) error {
	overwrite := false
	dryRun := false
	force := false
	var name string

	for _, a := range args {
		switch a {
		case "--overwrite":
			overwrite = true
		case "-n", "--dry-run":
			dryRun = true
		case "--force":
			force = true
		default:
			if name != "" {
				return fmt.Errorf("usage: grew link [--overwrite] [--dry-run] [--force] <formula>")
			}
			name = a
		}
	}

	if name == "" {
		return fmt.Errorf("usage: grew link [--overwrite] [--dry-run] [--force] <formula>")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	if !cel.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed", name)
	}

	ver, err := cel.InstalledVersion(name)
	if err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	f, err := loader.LoadByName(name)
	kegOnly := false
	if err == nil {
		kegOnly = f.KegOnly
	}

	if kegOnly && !force {
		fmt.Printf("Warning: %s is keg-only. Use --force to link anyway.\n", name)
	}

	lnk := &linker.Linker{Paths: paths}
	Logf("    Keg: %s\n", cel.KegPath(name, ver))
	opts := linker.LinkOpts{
		KegOnly:   kegOnly,
		Overwrite: overwrite,
		DryRun:    dryRun,
		Force:     force,
	}
	if err := lnk.LinkWithOpts(name, ver, opts); err != nil {
		return err
	}
	Logf("    opt/%s -> %s\n", name, cel.KegPath(name, ver))
	if !kegOnly || force {
		Logf("    Symlinked bin/, lib/, include/ contents\n")
	}

	if !dryRun {
		fmt.Printf("==> %s %s linked\n", name, ver)
	}
	return nil
}

func runUnlink(args []string) error {
	dryRun := false
	var name string

	for _, a := range args {
		switch a {
		case "-n", "--dry-run":
			dryRun = true
		default:
			if name != "" {
				return fmt.Errorf("usage: grew unlink [--dry-run] <formula>")
			}
			name = a
		}
	}

	if name == "" {
		return fmt.Errorf("usage: grew unlink [--dry-run] <formula>")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	if !cel.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed", name)
	}

	lnk := &linker.Linker{Paths: paths}
	if err := lnk.UnlinkWithOpts(name, linker.UnlinkOpts{DryRun: dryRun}); err != nil {
		return err
	}

	if dryRun {
		Logf("    (dry run, no changes made)\n")
	} else {
		Logf("    Removed symlinks from bin/, lib/, include/, opt/\n")
		fmt.Printf("==> %s unlinked\n", name)
	}
	return nil
}
