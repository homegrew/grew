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

	// Calculate all dependencies for all installed packages.
	isDependency := make(map[string]bool)
	for _, p := range packages {
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

	// Find orphaned dependencies: installed, not a dependency of anything else, AND not installed on request.
	var toRemove []string
	seen := make(map[string]bool)
	for _, p := range packages {
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true

		if !isDependency[p.Name] {
			// It's a leaf. Check if it was installed as a dependency.
			filtered := ctx.Cellar.FilterByManifest([]cellar.InstalledPackage{p}, false, true, false, false)
			if len(filtered) > 0 {
				toRemove = append(toRemove, p.Name)
			}
		}
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
