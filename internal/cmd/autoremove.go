package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"sort"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/flags"
)

func runAutoremove(args []string) error {
	slog.Debug("starting autoremove command execution")
	fs := flag.NewFlagSet("autoremove", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew autoremove [--dry-run]

Uninstall formulae that were only installed as a dependency of another formula
and are now no longer needed.

  -n, --dry-run                    List what would be uninstalled, but do not
                                   actually uninstall anything.
  -v, --verbose                    Make some output more verbose.
  -d, --debug                      Display any debugging information.
  -q, --quiet                      Make some output more quiet.
  -h, --help                       Show this message.
`)
	}

	flags.Register(fs)
	dryRun := fs.Bool("dry-run", false, "List what would be uninstalled, but do not actually uninstall anything.")
	fs.BoolVar(dryRun, "n", false, "List what would be uninstalled, but do not actually uninstall anything.")

	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	ctx, err := newInstallContext()
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
		if err := gatherDeps(ctx.Loader, f.Dependencies, deps, false); err != nil {
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
			filtered := filterByManifest([]cellar.InstalledPackage{p}, ctx.Cellar, false, true, false, false)
			if len(filtered) > 0 {
				toRemove = append(toRemove, p.Name)
			}
		}
	}

	if len(toRemove) == 0 {
		return nil
	}

	sort.Strings(toRemove)

	if *dryRun {
		fmt.Printf("Would uninstall: %s\n", joinVersions(toRemove))
		return nil
	}

	fmt.Printf("Autoremoving %d unneeded formulae:\n%s\n", len(toRemove), joinVersions(toRemove))

	for _, name := range toRemove {
		if err := uninstallFormula(ctx, name, false); err != nil {
			return err
		}
	}

	return nil
}
