package resolver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/homegrew/grew/pkg/depgraph"
)

// ErrCycleDetected is the sentinel returned when the graph contains a cycle.
var ErrCycleDetected = errors.New("dependency cycle detected")

// ErrMissingDependency is the sentinel returned when an edge target is absent from the graph.
var ErrMissingDependency = errors.New("missing dependency")

// ErrConflict is reserved for future version-conflict detection.
var ErrConflict = errors.New("dependency conflict")

// CycleError wraps the detected cycle paths and satisfies errors.Is(err, ErrCycleDetected).
type CycleError struct {
	Cycles [][]string
}

func (e *CycleError) Error() string {
	parts := make([]string, len(e.Cycles))
	for i, c := range e.Cycles {
		parts[i] = strings.Join(c, " -> ")
	}
	return fmt.Sprintf("dependency cycle detected: %s", strings.Join(parts, "; "))
}

func (e *CycleError) Is(target error) bool { return target == ErrCycleDetected }

// MissingError wraps the name of a dependency that is not present in the graph.
// It satisfies errors.Is(err, ErrMissingDependency).
type MissingError struct {
	Name string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("missing dependency: %q", e.Name)
}

func (e *MissingError) Is(target error) bool { return target == ErrMissingDependency }

// Resolver validates a *depgraph.Graph and produces an ordered install plan.
type Resolver struct {
	graph   *depgraph.Graph
	missing []string
	errors  []error
}

// New returns a Resolver operating on g.
func New(g *depgraph.Graph) *Resolver {
	return &Resolver{graph: g}
}

// Resolve validates the graph and returns nodes in dependency-first install
// order. Runtime dependencies appear before build dependencies at each level.
//
// Returns ErrMissingDependency if any edge target is absent from the graph, or
// ErrCycleDetected (as a *CycleError) if the graph contains a cycle.
func (r *Resolver) Resolve() ([]depgraph.NodeMeta, error) {
	r.missing = nil
	r.errors = nil

	// Collect missing nodes (edge targets with no corresponding Meta entry).
	seen := make(map[string]bool)
	for _, deps := range r.graph.Edges {
		for _, dep := range deps {
			if _, ok := r.graph.Meta[dep]; !ok && !seen[dep] {
				seen[dep] = true
				r.missing = append(r.missing, dep)
			}
		}
	}
	if len(r.missing) > 0 {
		return nil, &MissingError{Name: r.missing[0]}
	}

	sorted, err := r.graph.TopologicalSort()
	if errors.Is(err, depgraph.ErrCycleDetected) {
		cycles := r.graph.DetectCycles()
		return nil, &CycleError{Cycles: cycles}
	}
	if err != nil {
		return nil, err
	}

	result := make([]depgraph.NodeMeta, len(sorted))
	for i, name := range sorted {
		result[i] = r.graph.Meta[name]
	}
	return result, nil
}

// MissingPackages returns the names of declared dependencies not present as
// nodes in the graph. The slice is populated after a call to Resolve.
func (r *Resolver) MissingPackages() []string {
	return r.missing
}
