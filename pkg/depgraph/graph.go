package depgraph

import "github.com/homegrew/grew/pkg/formula"

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

// NewGraph returns an empty, ready-to-use Graph.
func NewGraph() *Graph {
	return &Graph{
		Edges: make(map[string][]string),
		Meta:  make(map[string]NodeMeta),
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
