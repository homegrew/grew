package cmd

import (
	"flag"
	"fmt"
	"sort"

	"github.com/homegrew/grew/internal/flags"
)

func runLeaves(args []string) error {
	fs := flag.NewFlagSet("leaves", flag.ContinueOnError)
	flags.Register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	packages, err := ctx.Cellar.List()
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		return nil
	}

	installedMap := make(map[string]bool)
	for _, p := range packages {
		installedMap[p.Name] = true
	}

	// Calculate all dependencies for all installed packages.
	// A package is a leaf if it is installed and is NOT a dependency
	// of any *other* installed package.
	isDependency := make(map[string]bool)

	for _, p := range packages {
		f, err := ctx.Loader.LoadByName(p.Name)
		if err != nil {
			// If formula is missing from tap, we can't determine its dependencies.
			// Homebrew generally ignores missing formulas or assumes they have no dependencies.
			continue
		}

		// Collect all recursive dependencies for this installed formula.
		deps := make(map[string]bool)
		if err := collectDeps(ctx.Loader, f.Dependencies, deps); err != nil {
			// Similar to above, if a dependency is missing, we just log/skip.
			continue
		}

		// Mark all collected dependencies as "is a dependency".
		for depName := range deps {
			isDependency[depName] = true
		}
	}

	// Collect the leaves.
	var leaves []string
	seen := make(map[string]bool)
	for _, p := range packages {
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true

		if !isDependency[p.Name] {
			leaves = append(leaves, p.Name)
		}
	}

	sort.Strings(leaves)

	for _, leaf := range leaves {
		fmt.Println(leaf)
	}

	return nil
}
