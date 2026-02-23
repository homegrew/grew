package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/tap"
)

func runDeps(args []string) error {
	tree := false
	all := false
	installed := false
	var targets []string

	for _, a := range args {
		switch a {
		case "--tree":
			tree = true
		case "--all":
			all = true
		case "--installed":
			installed = true
		default:
			targets = append(targets, a)
		}
	}

	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}
	loader := newLoader(paths.Taps)

	if all {
		formulas, err := loader.LoadAll()
		if err != nil {
			return err
		}
		for _, f := range formulas {
			targets = append(targets, f.Name)
		}
		sort.Strings(targets)
	} else if installed {
		cel := &cellar.Cellar{Path: paths.Cellar}
		pkgs, err := cel.List()
		if err != nil {
			return err
		}
		for _, p := range pkgs {
			targets = append(targets, p.Name)
		}
	}

	if len(targets) == 0 {
		return fmt.Errorf("usage: grew deps [--tree] [--all | --installed] <formula ...>")
	}

	for i, name := range targets {
		f, err := loader.LoadByName(name)
		if err != nil {
			return fmt.Errorf("formula not found: %s", name)
		}

		if tree {
			if len(targets) > 1 {
				fmt.Println(f.Name)
			}
			printTree(loader, f.Dependencies, "", make(map[string]bool))
		} else {
			allDeps := make(map[string]bool)
			if err := collectDeps(loader, f.Dependencies, allDeps); err != nil {
				return err
			}
			sorted := make([]string, 0, len(allDeps))
			for d := range allDeps {
				sorted = append(sorted, d)
			}
			sort.Strings(sorted)

			if len(targets) > 1 {
				fmt.Printf("%s: %s\n", name, strings.Join(sorted, " "))
			} else if len(sorted) == 0 {
				fmt.Printf("%s has no dependencies.\n", name)
			} else {
				for _, d := range sorted {
					fmt.Println(d)
				}
			}
		}

		if tree && i < len(targets)-1 {
			fmt.Println()
		}
	}

	return nil
}

func printTree(loader *formula.Loader, deps []string, prefix string, visited map[string]bool) {
	sort.Strings(deps)
	for i, dep := range deps {
		isLast := i == len(deps)-1
		connector := "├── "
		childPrefix := "│   "
		if isLast {
			connector = "└── "
			childPrefix = "    "
		}
		fmt.Printf("%s%s%s\n", prefix, connector, dep)

		if visited[dep] {
			continue
		}
		visited[dep] = true

		f, err := loader.LoadByName(dep)
		if err != nil || len(f.Dependencies) == 0 {
			continue
		}
		printTree(loader, f.Dependencies, prefix+childPrefix, visited)
	}
}

func collectDeps(loader *formula.Loader, deps []string, seen map[string]bool) error {
	for _, dep := range deps {
		if seen[dep] {
			continue
		}
		seen[dep] = true
		f, err := loader.LoadByName(dep)
		if err != nil {
			return fmt.Errorf("dependency %q not found", dep)
		}
		if err := collectDeps(loader, f.Dependencies, seen); err != nil {
			return err
		}
	}
	return nil
}
