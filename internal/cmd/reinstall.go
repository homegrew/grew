package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
)

func runReinstall(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grew reinstall <formula>")
	}
	name := args[0]

	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}
	dl := &downloader.Downloader{TmpDir: paths.Tmp}

	if !cel.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed (use 'grew install' instead)", name)
	}

	f, err := loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	fmt.Printf("==> Reinstalling %s %s\n", f.Name, f.Version)

	// Unlink and remove existing installation
	lnk.Unlink(name)
	Logf("    Unlinked %s\n", name)

	if err := cel.Uninstall(name); err != nil {
		return fmt.Errorf("remove old installation: %w", err)
	}
	Logf("    Removed old cellar entry\n")

	// Fresh install
	if err := installFormula(f, paths, cel, lnk, dl); err != nil {
		return err
	}

	return nil
}
