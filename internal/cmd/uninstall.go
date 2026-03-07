package cmd

import (
	"flag"
	"fmt"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/linker"
)

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	isCask := fs.Bool("cask", false, "Uninstall a cask")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 1 {
		return fmt.Errorf("usage: grew uninstall [--cask] <formula>")
	}

	if *isCask {
		return caskUninstall(fs.Arg(0))
	}

	name := fs.Arg(0)
	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	if !cel.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed", name)
	}

	lnk := &linker.Linker{Paths: paths}

	ver, _ := cel.InstalledVersion(name)
	Logf("    Cellar path: %s\n", cel.KegPath(name, ver))

	fmt.Printf("==> Unlinking %s...\n", name)
	lnk.Unlink(name)
	Logf("    Removed symlinks from bin/, lib/, include/, opt/\n")

	fmt.Printf("==> Removing %s...\n", name)
	if err := cel.Uninstall(name); err != nil {
		return err
	}

	fmt.Printf("==> %s uninstalled\n", name)
	return nil
}
