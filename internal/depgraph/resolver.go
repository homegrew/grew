package depgraph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/formula"
)

type CycleError struct {
	Chain []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("circular dependency detected: %s", strings.Join(e.Chain, " -> "))
}

type Resolver struct {
	Loader *formula.Loader
}

// Resolve returns formulas in installation order (dependencies first).
func (r *Resolver) Resolve(name string) ([]*formula.Formula, error) {
	// Build the full dependency graph by loading formulas transitively.
	// graph[A] = [B, C] means "A depends on B and C".
	graph := map[string][]string{}
	formulas := map[string]*formula.Formula{}

	queue := []string{name}
	visited := map[string]bool{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true

		f, err := r.Loader.LoadByName(current)
		if err != nil {
			return nil, fmt.Errorf("dependency %q required by %q not found: %w", current, name, err)
		}
		formulas[current] = f
		graph[current] = f.Dependencies

		for _, dep := range f.Dependencies {
			if !visited[dep] {
				queue = append(queue, dep)
			}
		}
	}

	sorted, err := topoSort(graph)
	if err != nil {
		return nil, err
	}

	result := make([]*formula.Formula, len(sorted))
	for i, n := range sorted {
		result[i] = formulas[n]
	}
	return result, nil
}

// topoSort performs Kahn's algorithm with a pre-built reverse adjacency
// list for O(V+E) performance. Returns nodes in dependency-first order.
func topoSort(graph map[string][]string) ([]string, error) {
	// inDegree[X] = number of unsatisfied dependencies of X.
	inDegree := make(map[string]int, len(graph))
	// reverse[B] = list of nodes that depend on B.
	reverse := make(map[string][]string, len(graph))

	for node, deps := range graph {
		if _, exists := inDegree[node]; !exists {
			inDegree[node] = 0
		}
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], node)
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
		return nil, &CycleError{Chain: findCycle(graph, inDegree)}
	}
	return sorted, nil
}

// findCycle traces through unresolved nodes to report an actual cycle path.
func findCycle(graph map[string][]string, inDegree map[string]int) []string {
	// Pick any unresolved node as start.
	var start string
	for node, deg := range inDegree {
		if deg > 0 {
			start = node
			break
		}
	}
	if start == "" {
		return nil
	}

	// DFS to find the cycle.
	visited := map[string]bool{}
	var path []string
	if traceCycle(graph, start, visited, &path) {
		return path
	}
	// Fallback: just list remaining nodes.
	var remaining []string
	for node, deg := range inDegree {
		if deg > 0 {
			remaining = append(remaining, node)
		}
	}
	sort.Strings(remaining)
	return remaining
}

func traceCycle(graph map[string][]string, node string, visited map[string]bool, path *[]string) bool {
	if visited[node] {
		// Found cycle — trim path to just the cycle.
		for i, n := range *path {
			if n == node {
				*path = append((*path)[i:], node)
				return true
			}
		}
		return false
	}
	visited[node] = true
	*path = append(*path, node)
	for _, dep := range graph[node] {
		if traceCycle(graph, dep, visited, path) {
			return true
		}
	}
	*path = (*path)[:len(*path)-1]
	return false
}
