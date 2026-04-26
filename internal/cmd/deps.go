package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/formula"
)

func runDeps(args []string) error {
	slog.Debug("starting deps command execution")
	slog.Debug("starting deps command execution")
	fs := flag.NewFlagSet("deps", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew deps [options] <formula ...>

Show dependencies for formulas. When given multiple formula arguments, show the
intersection of dependencies for each formula.

Options:
  -n, --topological   Sort dependencies in topological order.
  -1, --direct,       Show only the direct dependencies declared in the formula.
      --declared
      --union         Show the union of dependencies for multiple formulas,
                      instead of the intersection.
      --include-build Include build dependencies for formulas.
      --for-each      List dependencies for each provided formula.
      --tree          Show dependencies as a tree.
      --all           Show dependencies for all formulas.
      --installed     Show dependencies for installed formulas.
      --missing       Show only missing dependencies.
  -v, --verbose       Show detailed output.
  -d, --debug         Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	tree := fs.Bool("tree", false, "Show dependencies as a tree")
	all := fs.Bool("all", false, "Show dependencies for all formulas")
	installed := fs.Bool("installed", false, "Show dependencies for installed formulas")

	topo := fs.Bool("n", false, "Sort dependencies in topological order")
	fs.BoolVar(topo, "topological", false, "Sort dependencies in topological order")

	direct := fs.Bool("1", false, "Show only the direct dependencies declared in the formula")
	fs.BoolVar(direct, "direct", false, "Show only the direct dependencies declared in the formula")
	fs.BoolVar(direct, "declared", false, "Show only the direct dependencies declared in the formula")

	union := fs.Bool("union", false, "Show the union of dependencies for multiple formula")
	includeBuild := fs.Bool("include-build", false, "Include :build dependencies for formula")
	forEach := fs.Bool("for-each", false, "List dependencies for each provided formula")
	missing := fs.Bool("missing", false, "Show only missing dependencies")

	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	targets := fs.Args()

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	filterMissing := func(deps []string) []string {
		if !*missing {
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

	if *all {
		formulas, err := ctx.Loader.LoadAll()
		if err != nil {
			return err
		}
		for _, f := range formulas {
			targets = append(targets, f.Name)
		}
		sort.Strings(targets)
	} else if *installed {
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

	if *tree {
		for i, name := range targets {
			f, err := ctx.Loader.LoadByName(name)
			if err != nil {
				return fmt.Errorf("formula not found: %s", name)
			}
			fmt.Println(f.Name)
			deps := f.Dependencies
			if *includeBuild {
				deps = append(deps, f.BuildDependencies...)
			}
			printTree(ctx.Loader, deps, "", make(map[string]bool), *includeBuild, filterMissing)
			if i < len(targets)-1 {
				fmt.Println()
			}
		}
		return nil
	}

	if *forEach || len(targets) == 1 {
		for _, name := range targets {
			deps, err := getDepsForFormula(ctx.Loader, name, *direct, *includeBuild, *topo)
			if err != nil {
				return err
			}
			deps = filterMissing(deps)
			if *forEach {
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
	if *union {
		depSet := make(map[string]bool)
		for _, name := range targets {
			deps, err := getDepsForFormula(ctx.Loader, name, *direct, *includeBuild, false)
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
			deps, err := getDepsForFormula(ctx.Loader, name, *direct, *includeBuild, false)
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

	if *topo {
		finalDeps = sortTopologically(ctx.Loader, finalDeps)
	} else {
		sort.Strings(finalDeps)
	}

	finalDeps = filterMissing(finalDeps)

	for _, d := range finalDeps {
		fmt.Println(d)
	}

	return nil
}

func getDepsForFormula(loader *formula.Loader, name string, direct, includeBuild, topo bool) ([]string, error) {
	f, err := loader.LoadByName(name)
	if err != nil {
		return nil, fmt.Errorf("formula not found: %s", name)
	}

	deps := f.Dependencies
	if includeBuild {
		deps = append(deps, f.BuildDependencies...)
	}

	if direct {
		if topo {
			return sortTopologically(loader, deps), nil
		}
		sort.Strings(deps)
		return deps, nil
	}

	allDeps := make(map[string]bool)
	if err := gatherDeps(loader, deps, allDeps, includeBuild); err != nil {
		return nil, err
	}

	var result []string
	for d := range allDeps {
		result = append(result, d)
	}

	if topo {
		return sortTopologically(loader, result), nil
	}
	sort.Strings(result)
	return result, nil
}

func sortTopologically(loader *formula.Loader, deps []string) []string {
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
		f, err := loader.LoadByName(d)
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

func printTree(loader *formula.Loader, deps []string, prefix string, visited map[string]bool, includeBuild bool, filterMissing func([]string) []string) {
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

		f, err := loader.LoadByName(dep)
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
		printTree(loader, subDeps, prefix+childPrefix, visited, includeBuild, filterMissing)
	}
}

func gatherDeps(loader *formula.Loader, deps []string, seen map[string]bool, includeBuild bool) error {
	for _, dep := range deps {
		if seen[dep] {
			continue
		}
		seen[dep] = true
		f, err := loader.LoadByName(dep)
		if err != nil {
			return fmt.Errorf("dependency %q not found", dep)
		}
		subDeps := f.Dependencies
		if includeBuild {
			subDeps = append(subDeps, f.BuildDependencies...)
		}
		if err := gatherDeps(loader, subDeps, seen, includeBuild); err != nil {
			return err
		}
	}
	return nil
}
