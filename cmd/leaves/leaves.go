package leaves

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/spf13/cobra"
	"github.com/homegrew/grew/internal/cmd"
)

var (
	leavesOnRequest bool
	leavesAsDep     bool
)

var Command = &cobra.Command{
	Use:   "leaves",
	Short: "List installed formulas that are not dependencies of another installed formula",
	Long: `List installed formulas that are not dependencies of any other installed formula.`,
	RunE: func(c *cobra.Command, args []string) error {
		return RunLeaves(args)
	},
}

func init() {
	Command.Flags().BoolVarP(&leavesOnRequest, "installed-on-request", "r", false, "Only show leaves that were explicitly installed on request")
	Command.Flags().BoolVarP(&leavesAsDep, "installed-as-dependency", "p", false, "Only show leaves that were installed automatically as dependencies")
}

func RunLeaves(args []string) error {
	slog.Debug("starting leaves command execution")
	
	if leavesOnRequest && leavesAsDep {
		return fmt.Errorf("--installed-on-request and --installed-as-dependency are mutually exclusive")
	}

	ctx, err := cmd.NewReadContext()
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

	if leavesOnRequest || leavesAsDep {
		packages = cmd.FilterByManifest(packages, ctx.Cellar, leavesOnRequest, leavesAsDep, false, false)
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
		if err := cmd.GatherDeps(ctx.Loader, f.Dependencies, deps, false); err != nil {
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
