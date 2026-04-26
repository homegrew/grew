package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"sort"

	"github.com/homegrew/grew/internal/flags"
)

func runLeaves(args []string) error {
	slog.Debug("starting leaves command execution")
	slog.Debug("starting leaves command execution")
	fs := flag.NewFlagSet("leaves", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew leaves [options]

List installed formulas that are not dependencies of any other installed formula.

Options:
  -r, --installed-on-request
                Only show leaves that were explicitly installed on request.
  -p, --installed-as-dependency
                Only show leaves that were installed automatically as dependencies
                (these are orphaned dependencies that can likely be removed).
  -v, --verbose Show detailed output.
  -d, --debug   Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	onRequest := fs.Bool("installed-on-request", false, "Only show leaves that were installed on request")
	fs.BoolVar(onRequest, "r", false, "Only show leaves that were installed on request")
	asDep := fs.Bool("installed-as-dependency", false, "Only show leaves that were installed as dependencies")
	fs.BoolVar(asDep, "p", false, "Only show leaves that were installed as dependencies")

	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if *onRequest && *asDep {
		return fmt.Errorf("--installed-on-request and --installed-as-dependency are mutually exclusive")
	}

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

	if *onRequest || *asDep {
		packages = filterByManifest(packages, ctx.Cellar, *onRequest, *asDep, false, false)
		if len(packages) == 0 {
			return nil
		}
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

		// Mark direct dependencies immediately
		for _, dep := range f.Dependencies {
			isDependency[dep] = true
		}

		// Collect all recursive dependencies for this installed formula.
		deps := make(map[string]bool)
		if err := gatherDeps(ctx.Loader, f.Dependencies, deps, false); err != nil {
			// If collectDeps fails (e.g. missing transitive dependency), log it
			// but continue to merge whatever partial dependencies we collected.
			slog.Debug("failed to collect all recursive dependencies", "package", p.Name, "error", err)
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
