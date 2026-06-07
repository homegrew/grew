package depgraph

import (
	"errors"
	"fmt"
	"sort"

	"github.com/homegrew/grew/pkg/formula"
)

// ErrCycleDetected is returned by TopologicalSort when the graph contains a cycle.
var ErrCycleDetected = errors.New("dependency cycle detected")

// NodeMeta holds per-node metadata in the dependency graph.
type NodeMeta struct {
	Name       string
	Version    string
	Source     string
	Kind       formula.DepKind
	BuildHooks []string
	TestHook   string
	Caveats    string
}

// Graph is a directed dependency graph with per-node metadata.
type Graph struct {
	// Edges maps each node name to its direct dependency names.
	Edges map[string][]string
	Meta  map[string]NodeMeta
}

// New returns an empty, ready-to-use Graph.
func New() *Graph {
	return &Graph{
		Edges: make(map[string][]string),
		Meta:  make(map[string]NodeMeta),
	}
}

// AddNode inserts or replaces the node for meta.Name. If no edge list exists
// for this node yet, an empty one is initialised.
func (g *Graph) AddNode(meta NodeMeta) {
	g.Meta[meta.Name] = meta
	if _, ok := g.Edges[meta.Name]; !ok {
		g.Edges[meta.Name] = nil
	}
}

// AddEdge appends to to the edge list of from. Returns an error if from has
// not been added as a node via AddNode or AddFormula.
func (g *Graph) AddEdge(from, to string) error {
	if _, ok := g.Meta[from]; !ok {
		return fmt.Errorf("depgraph: node %q not found; add it before adding edges", from)
	}
	g.Edges[from] = append(g.Edges[from], to)
	return nil
}

// RemoveNode deletes the node and its outgoing edges, and removes name from
// the edge lists of all other nodes.
func (g *Graph) RemoveNode(name string) {
	delete(g.Meta, name)
	delete(g.Edges, name)
	for n, deps := range g.Edges {
		filtered := deps[:0]
		for _, d := range deps {
			if d != name {
				filtered = append(filtered, d)
			}
		}
		g.Edges[n] = filtered
	}
}

// AddFormula inserts f into the graph. Edge targets are derived from f.Deps
// (structured); when f.Deps is empty the legacy f.Dependencies string slice is
// used instead. The node Kind defaults to DepRuntime; callers may overwrite
// Meta[f.Name].Kind when the node's role in the larger graph is known.
func (g *Graph) AddFormula(f *formula.Formula) {
	source := ""
	if f.Source != nil {
		source = f.Source.URL
	}
	g.Meta[f.Name] = NodeMeta{
		Name:       f.Name,
		Version:    f.Version,
		Source:     source,
		Kind:       formula.DepRuntime,
		BuildHooks: f.BuildHooks,
		TestHook:   f.TestHook,
		Caveats:    f.Caveats,
	}

	if len(f.Deps) > 0 {
		targets := make([]string, len(f.Deps))
		for i, d := range f.Deps {
			targets[i] = d.Name
		}
		g.Edges[f.Name] = targets
	} else {
		g.Edges[f.Name] = append([]string(nil), f.Dependencies...)
	}
}

// DetectCycles returns each cycle in the graph as an ordered path of node
// names (start repeated at the end to close the loop), or nil if acyclic.
// Uses DFS with a recursion stack for exact cycle extraction.
func (g *Graph) DetectCycles() [][]string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(g.Edges))
	var cycles [][]string
	stack := make([]string, 0, len(g.Edges))

	nodes := sortedKeys(g.Edges)

	var dfs func(node string)
	dfs = func(node string) {
		color[node] = gray
		stack = append(stack, node)

		deps := g.Edges[node]
		sorted := make([]string, len(deps))
		copy(sorted, deps)
		sort.Strings(sorted)

		for _, dep := range sorted {
			switch color[dep] {
			case gray:
				cycles = append(cycles, extractCycle(stack, dep))
			case white:
				dfs(dep)
			}
		}

		stack = stack[:len(stack)-1]
		color[node] = black
	}

	for _, node := range nodes {
		if color[node] == white {
			dfs(node)
		}
	}
	return cycles
}

// TopologicalSort returns nodes in dependency-first order using Kahn's
// algorithm. When multiple nodes are ready at the same level, runtime nodes
// (Kind != DepBuild) are emitted before build nodes, with alphabetical
// ordering within each group. Returns ErrCycleDetected if the graph has a
// cycle.
func (g *Graph) TopologicalSort() ([]string, error) {
	inDegree := make(map[string]int, len(g.Edges))
	reverse := make(map[string][]string, len(g.Edges))

	for node, deps := range g.Edges {
		if _, exists := inDegree[node]; !exists {
			inDegree[node] = 0
		}
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], node)
			inDegree[node]++
		}
	}

	ready := make([]string, 0, len(inDegree))
	for node, deg := range inDegree {
		if deg == 0 {
			ready = append(ready, node)
		}
	}
	g.sortReady(ready)

	sorted := make([]string, 0, len(inDegree))
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
			g.sortReady(newReady)
			ready = append(ready, newReady...)
		}
	}

	if len(sorted) != len(g.Edges) {
		return nil, ErrCycleDetected
	}
	return sorted, nil
}

// sortReady sorts a slice of node names in-place: runtime nodes first, build
// nodes last; alphabetical within each group.
func (g *Graph) sortReady(nodes []string) {
	sort.Slice(nodes, func(i, j int) bool {
		ki := g.Meta[nodes[i]].Kind == formula.DepBuild
		kj := g.Meta[nodes[j]].Kind == formula.DepBuild
		if ki != kj {
			return !ki // runtime (false) before build (true)
		}
		return nodes[i] < nodes[j]
	})
}

// extractCycle reconstructs the cycle path from the DFS stack. The returned
// slice starts at start and ends with start (closed loop).
func extractCycle(stack []string, start string) []string {
	for i, n := range stack {
		if n == start {
			cycle := make([]string, len(stack)-i+1)
			copy(cycle, stack[i:])
			cycle[len(cycle)-1] = start
			return cycle
		}
	}
	return []string{start, start}
}

// sortedKeys returns map keys in sorted order.
func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
