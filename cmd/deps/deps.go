package deps

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/context"
	"github.com/spf13/cobra"
)

var (
	depsTree         bool
	depsAll          bool
	depsInstalled    bool
	depsTopological  bool
	depsDirect       bool
	depsUnion        bool
	depsIncludeBuild bool
	depsForEach      bool
	depsMissing      bool
)

var Command = &cobra.Command{
	Use:   "deps [flags] <formula ...>",
	Short: "Show dependencies for formulas",
	Long: `Show dependencies for one or more formulas. By default shows all
transitive dependencies. Use --tree for a visual tree view.

Examples:
  grew deps jq
  grew deps --tree jq
  grew deps --all
  grew deps --installed`,
	RunE: func(c *cobra.Command, args []string) error {
		return runDeps(args)
	},
}

func init() {
	Command.Flags().BoolVar(&depsTree, "tree", false, "Show dependencies as a tree")
	Command.Flags().BoolVar(&depsAll, "all", false, "Show dependencies for all formulas")
	Command.Flags().BoolVar(&depsInstalled, "installed", false, "Show dependencies for installed formulas")
	Command.Flags().BoolVarP(&depsTopological, "topological", "n", false, "Sort dependencies in topological order")
	Command.Flags().BoolVarP(&depsDirect, "direct", "1", false, "Show only the direct dependencies declared in the formula")
	Command.Flags().BoolVar(&depsUnion, "union", false, "Show the union of dependencies for multiple formulas")
	Command.Flags().BoolVar(&depsIncludeBuild, "include-build", false, "Include build dependencies for formulas")
	Command.Flags().BoolVar(&depsForEach, "for-each", false, "List dependencies for each provided formula")
	Command.Flags().BoolVar(&depsMissing, "missing", false, "Show only missing dependencies")
}

func runDeps(args []string) error {
	slog.Debug("starting deps command execution")

	targets := args

	ctx, err := context.New()
	if err != nil {
		return err
	}

	filterMissing := func(deps []string) []string {
		if !depsMissing {
			return deps
		}
		var out []string
		for _, d := range deps {
			if !ctx.Cellar.IsInstalled(d) {
				out = append(out, d)
			}
		}
		return out
	}

	if depsAll {
		formulas, err := ctx.Loader.LoadAll()
		if err != nil {
			return err
		}
		for _, f := range formulas {
			targets = append(targets, f.Name)
		}
		sort.Strings(targets)
	} else if depsInstalled {
		pkgs, err := ctx.Cellar.List()
		if err != nil {
			return err
		}
		for _, p := range pkgs {
			targets = append(targets, p.Name)
		}
	}

	if len(targets) == 0 {
		return fmt.Errorf("usage: grew deps [options] <formula ...>")
	}

	if depsTree {
		for i, name := range targets {
			f, err := ctx.LoadFormula(name)
			if err != nil {
				return fmt.Errorf("formula not found: %s", name)
			}
			fmt.Println(f.Name)
			deps := f.Dependencies
			if depsIncludeBuild {
				deps = append(deps, f.BuildDependencies...)
			}
			printTree(ctx, deps, "", make(map[string]bool), depsIncludeBuild, filterMissing)
			if i < len(targets)-1 {
				fmt.Println()
			}
		}
		return nil
	}

	if depsForEach || len(targets) == 1 {
		for _, name := range targets {
			deps, err := getDepsForFormula(ctx, name, depsDirect, depsIncludeBuild, depsTopological)
			if err != nil {
				return err
			}
			deps = filterMissing(deps)
			if depsForEach {
				fmt.Printf("%s: %s\n", name, strings.Join(deps, " "))
			} else {
				for _, d := range deps {
					fmt.Println(d)
				}
			}
		}
		return nil
	}

	// Multiple targets, intersection or union
	var finalDeps []string
	if depsUnion {
		depSet := make(map[string]bool)
		for _, name := range targets {
			deps, err := getDepsForFormula(ctx, name, depsDirect, depsIncludeBuild, false)
			if err != nil {
				return err
			}
			for _, d := range deps {
				depSet[d] = true
			}
		}
		for d := range depSet {
			finalDeps = append(finalDeps, d)
		}
	} else {
		// Intersection
		var isect map[string]bool
		for i, name := range targets {
			deps, err := getDepsForFormula(ctx, name, depsDirect, depsIncludeBuild, false)
			if err != nil {
				return err
			}
			currentSet := make(map[string]bool)
			for _, d := range deps {
				currentSet[d] = true
			}
			if i == 0 {
				isect = currentSet
			} else {
				for d := range isect {
					if !currentSet[d] {
						delete(isect, d)
					}
				}
			}
		}
		for d := range isect {
			finalDeps = append(finalDeps, d)
		}
	}

	if depsTopological {
		finalDeps = sortTopologically(ctx, finalDeps)
	} else {
		sort.Strings(finalDeps)
	}

	finalDeps = filterMissing(finalDeps)

	for _, d := range finalDeps {
		fmt.Println(d)
	}

	return nil
}

func getDepsForFormula(ctx *context.Context, name string, direct, includeBuild, topo bool) ([]string, error) {
	f, err := ctx.LoadFormula(name)
	if err != nil {
		return nil, fmt.Errorf("formula not found: %s", name)
	}

	deps := f.Dependencies
	if includeBuild {
		deps = append(deps, f.BuildDependencies...)
	}

	if direct {
		if topo {
			return sortTopologically(ctx, deps), nil
		}
		sort.Strings(deps)
		return deps, nil
	}

	allDeps := make(map[string]bool)
	if err := gatherDepsWithFallback(ctx, deps, allDeps, includeBuild); err != nil {
		return nil, err
	}

	var result []string
	for d := range allDeps {
		result = append(result, d)
	}

	if topo {
		return sortTopologically(ctx, result), nil
	}
	sort.Strings(result)
	return result, nil
}

func gatherDepsWithFallback(ctx *context.Context, deps []string, allDeps map[string]bool, includeBuild bool) error {
	for _, dep := range deps {
		if allDeps[dep] {
			continue
		}
		allDeps[dep] = true
		f, err := ctx.LoadFormula(dep)
		if err != nil {
			return err
		}
		subDeps := f.Dependencies
		if includeBuild {
			subDeps = append(subDeps, f.BuildDependencies...)
		}
		if err := gatherDepsWithFallback(ctx, subDeps, allDeps, includeBuild); err != nil {
			return err
		}
	}
	return nil
}

func sortTopologically(ctx *context.Context, deps []string) []string {
	if len(deps) <= 1 {
		return deps
	}
	// We can use depgraph.Resolver for a dummy node that depends on all deps.
	// But it returns all transitive dependencies. We just want to sort the GIVEN deps.
	// So we extract the subgraph and sort it.
	graph := make(map[string][]string)
	depSet := make(map[string]bool)
	for _, d := range deps {
		depSet[d] = true
	}

	for _, d := range deps {
		f, err := ctx.LoadFormula(d)
		if err != nil {
			graph[d] = []string{}
			continue
		}
		var edges []string
		for _, dep := range f.Dependencies {
			if depSet[dep] {
				edges = append(edges, dep)
			}
		}
		graph[d] = edges
	}

	// Kahn's algorithm
	inDegree := make(map[string]int)
	reverse := make(map[string][]string)

	for node, edges := range graph {
		if _, exists := inDegree[node]; !exists {
			inDegree[node] = 0
		}
		for _, edge := range edges {
			reverse[edge] = append(reverse[edge], node)
			inDegree[node]++
		}
	}

	var ready []string
	for node, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, node)
		}
	}
	sort.Strings(ready)

	var sorted []string
	for len(ready) > 0 {
		node := ready[0]
		ready = ready[1:]
		sorted = append(sorted, node)

		var newReady []string
		for _, dependent := range reverse[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				newReady = append(newReady, dependent)
			}
		}
		if len(newReady) > 0 {
			sort.Strings(newReady)
			ready = append(ready, newReady...)
		}
	}

	if len(sorted) != len(graph) {
		// Cycle detected, fallback to alphabetical
		sort.Strings(deps)
		return deps
	}
	return sorted
}

func printTree(ctx *context.Context, deps []string, prefix string, visited map[string]bool, includeBuild bool, filterMissing func([]string) []string) {
	deps = filterMissing(deps)
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

		f, err := ctx.LoadFormula(dep)
		if err != nil {
			continue
		}

		subDeps := f.Dependencies
		if includeBuild {
			subDeps = append(subDeps, f.BuildDependencies...)
		}
		if len(subDeps) == 0 {
			continue
		}
		printTree(ctx, subDeps, prefix+childPrefix, visited, includeBuild, filterMissing)
	}
}

