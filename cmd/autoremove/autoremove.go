package autoremove

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/homegrew/grew/pkg/cellar"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/version"
	"github.com/spf13/cobra"
)

var autoremoveDryRun bool

var Command = &cobra.Command{
	Use:   "autoremove",
	Short: "Uninstall formulae that were only installed as a dependency",
	Long: `Uninstall formulae that were only installed as a dependency of another formula
and are now no longer needed.`,
	RunE: func(c *cobra.Command, args []string) error {
		return RunAutoremove(args)
	},
}

func init() {
	Command.Flags().BoolVarP(&autoremoveDryRun, "dry-run", "n", false, "List what would be uninstalled, but do not actually uninstall anything.")
}

func RunAutoremove(args []string) error {
	slog.Debug("starting autoremove command execution")
	
	ctx, err := context.NewInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	packages, err := ctx.Cellar.List()
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		return nil
	}

	// Iteratively find orphaned dependencies. Each pass may expose new orphans
	// (e.g. removing B, which depended on C, makes C a new orphan). Repeat
	// until no new candidates are found.
	markedForRemoval := make(map[string]bool)
	for {
		isDependency := make(map[string]bool)
		for _, p := range packages {
			if markedForRemoval[p.Name] {
				continue
			}
			f, err := ctx.Loader.LoadByName(p.Name)
			if err != nil {
				continue
			}
			for _, dep := range f.Dependencies {
				isDependency[dep] = true
			}
			deps := make(map[string]bool)
			if err := ctx.Loader.GatherDeps(f.Dependencies, deps, false); err != nil {
				slog.Debug("failed to collect all recursive dependencies", "package", p.Name, "error", err)
			}
			for depName := range deps {
				isDependency[depName] = true
			}
		}

		newOrphans := 0
		seen := make(map[string]bool)
		for _, p := range packages {
			if seen[p.Name] || markedForRemoval[p.Name] {
				continue
			}
			seen[p.Name] = true
			if !isDependency[p.Name] {
				filtered := ctx.Cellar.FilterByManifest([]cellar.InstalledPackage{p}, false, true, false, false)
				if len(filtered) > 0 {
					markedForRemoval[p.Name] = true
					newOrphans++
				}
			}
		}

		if newOrphans == 0 {
			break
		}
	}

	var toRemove []string
	for name := range markedForRemoval {
		toRemove = append(toRemove, name)
	}

	if len(toRemove) == 0 {
		return nil
	}

	sort.Strings(toRemove)

	if autoremoveDryRun {
		fmt.Printf("Would uninstall: %s\n", version.Join(toRemove))
		return nil
	}

	fmt.Printf("Autoremoving %d unneeded formulae:\n%s\n", len(toRemove), version.Join(toRemove))

	for _, name := range toRemove {
		if err := ctx.UninstallFormula(name, false); err != nil {
			return err
		}
	}

	return nil
}
