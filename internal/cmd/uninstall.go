package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/linker"
)

func runUninstall(args []string) error {
	isCask := false
	var remaining []string
	for _, a := range args {
		if a == "--cask" {
			isCask = true
		} else {
			remaining = append(remaining, a)
		}
	}

	if len(remaining) != 1 {
		return fmt.Errorf("usage: grew uninstall [--cask] <formula>")
	}

	if isCask {
		return caskUninstall(remaining[0])
	}

	name := remaining[0]
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
