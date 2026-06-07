package resolver

import (
	"errors"
	"testing"

	"github.com/homegrew/grew/pkg/depgraph"
	"github.com/homegrew/grew/pkg/formula"
)

func buildGraph(nodes []depgraph.NodeMeta, edges [][2]string) *depgraph.Graph {
	g := depgraph.New()
	for _, m := range nodes {
		g.AddNode(m)
	}
	for _, e := range edges {
		_ = g.AddEdge(e[0], e[1])
	}
	return g
}

func TestResolve_Linear(t *testing.T) {
	t.Parallel()
	g := buildGraph(
		[]depgraph.NodeMeta{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		[][2]string{{"a", "b"}, {"b", "c"}},
	)
	plan, err := New(g).Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(plan))
	}
	order := indexMap(plan)
	if order["c"] >= order["b"] {
		t.Errorf("c should precede b")
	}
	if order["b"] >= order["a"] {
		t.Errorf("b should precede a")
	}
}

func TestResolve_Diamond(t *testing.T) {
	t.Parallel()
	g := buildGraph(
		[]depgraph.NodeMeta{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}},
		[][2]string{{"a", "b"}, {"a", "c"}, {"b", "d"}, {"c", "d"}},
	)
	plan, err := New(g).Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan) != 4 {
		t.Fatalf("want 4 nodes, got %d: %v", len(plan), plan)
	}
	order := indexMap(plan)
	if order["d"] >= order["b"] || order["d"] >= order["c"] {
		t.Errorf("d should precede b and c, order: %v", order)
	}
}

func TestResolve_Cycle(t *testing.T) {
	t.Parallel()
	g := buildGraph(
		[]depgraph.NodeMeta{{Name: "x"}, {Name: "y"}},
		[][2]string{{"x", "y"}, {"y", "x"}},
	)
	_, err := New(g).Resolve()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrCycleDetected) {
		t.Errorf("want ErrCycleDetected, got %T: %v", err, err)
	}
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("want *CycleError, got %T", err)
	}
	if len(ce.Cycles) == 0 {
		t.Error("CycleError should contain at least one cycle path")
	}
	// Both x and y must appear in the reported cycle.
	names := make(map[string]bool)
	for _, n := range ce.Cycles[0] {
		names[n] = true
	}
	if !names["x"] || !names["y"] {
		t.Errorf("cycle should contain x and y, got %v", ce.Cycles[0])
	}
}

func TestResolve_MissingDependency(t *testing.T) {
	t.Parallel()
	g := buildGraph(
		[]depgraph.NodeMeta{{Name: "a"}},
		[][2]string{{"a", "ghost"}},
	)
	r := New(g)
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrMissingDependency) {
		t.Errorf("want ErrMissingDependency, got %T: %v", err, err)
	}
	var me *MissingError
	if !errors.As(err, &me) {
		t.Fatalf("want *MissingError, got %T", err)
	}
	if me.Name != "ghost" {
		t.Errorf("MissingError.Name = %q, want ghost", me.Name)
	}
	if pkgs := r.MissingPackages(); len(pkgs) == 0 || pkgs[0] != "ghost" {
		t.Errorf("MissingPackages = %v, want [ghost]", pkgs)
	}
}

func TestResolve_RuntimeBeforeBuild(t *testing.T) {
	t.Parallel()
	g := buildGraph(
		[]depgraph.NodeMeta{
			{Name: "top"},
			{Name: "rt", Kind: formula.DepRuntime},
			{Name: "bd", Kind: formula.DepBuild},
		},
		[][2]string{{"top", "rt"}, {"top", "bd"}},
	)
	plan, err := New(g).Resolve()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	order := indexMap(plan)
	if order["rt"] >= order["bd"] {
		t.Errorf("runtime (pos %d) should precede build (pos %d)", order["rt"], order["bd"])
	}
}

func indexMap(plan []depgraph.NodeMeta) map[string]int {
	m := make(map[string]int, len(plan))
	for i, n := range plan {
		m[n.Name] = i
	}
	return m
}
