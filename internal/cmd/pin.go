package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
)

func runPin(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grew pin <formula>")
	}
	name := args[0]

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	if !cel.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed", name)
	}

	if cel.IsPinned(name) {
		fmt.Printf("%s is already pinned.\n", name)
		return nil
	}

	if err := cel.Pin(name); err != nil {
		return err
	}
	fmt.Printf("Pinned %s (will not be upgraded).\n", name)
	return nil
}

func runUnpin(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grew unpin <formula>")
	}
	name := args[0]

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	if !cel.IsInstalled(name) {
		return fmt.Errorf("formula %q is not installed", name)
	}

	if !cel.IsPinned(name) {
		fmt.Printf("%s is not pinned.\n", name)
		return nil
	}

	if err := cel.Unpin(name); err != nil {
		return err
	}
	fmt.Printf("Unpinned %s.\n", name)
	return nil
}
