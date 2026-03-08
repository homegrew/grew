package cmd

import (
	"fmt"
)

func runReinstall(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grew reinstall <formula>")
	}
	name := args[0]

	ctx, err := newInstallContext()
	if err != nil {
		return err
	}

	if !ctx.Cellar.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed (use 'grew install' instead)", name)
	}

	f, err := ctx.Loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	fmt.Printf("==> Reinstalling %s %s\n", f.Name, f.Version)

	// Unlink and remove existing installation
	ctx.Linker.Unlink(name)
	Logf("    Unlinked %s\n", name)

	if err := ctx.Cellar.Uninstall(name); err != nil {
		return fmt.Errorf("remove old installation: %w", err)
	}
	Logf("    Removed old cellar entry\n")

	// Fresh install
	if err := installFormula(f, ctx, false, false); err != nil {
		return err
	}

	return nil
}
