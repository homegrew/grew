package depgraph

import (
	"errors"
	"testing"

	"github.com/homegrew/grew/pkg/formula"
)

const graphFixtureYAML = `
name: mypkg
version: "2.3.1"
description: "test"
homepage: "https://example.com"
license: "MIT"
url:
  darwin_arm64: "https://example.com/mypkg"
sha256:
  darwin_arm64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
install:
  type: binary
  binary_name: mypkg
deps:
  - name: libfoo
    kind: 0
  - name: cmake
    kind: 1
  - name: bats
    kind: 2
build_hooks:
  - configure
  - make
test_hook: run-tests
caveats: "Add mypkg to PATH."
`

func TestAddFormula_NodeMeta(t *testing.T) {
	t.Parallel()
	f, err := formula.Parse([]byte(graphFixtureYAML))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	g := New()
	g.AddFormula(f)

	meta, ok := g.Meta["mypkg"]
	if !ok {
		t.Fatal("NodeMeta not found for mypkg")
	}
	if meta.Name != "mypkg" {
		t.Errorf("Name = %q, want mypkg", meta.Name)
	}
	if meta.Version != "2.3.1" {
		t.Errorf("Version = %q, want 2.3.1", meta.Version)
	}
	if meta.Kind != formula.DepRuntime {
		t.Errorf("Kind = %v, want DepRuntime", meta.Kind)
	}
	if len(meta.BuildHooks) != 2 || meta.BuildHooks[0] != "configure" {
		t.Errorf("BuildHooks = %v", meta.BuildHooks)
	}
	if meta.TestHook != "run-tests" {
		t.Errorf("TestHook = %q, want run-tests", meta.TestHook)
	}
	if meta.Caveats != "Add mypkg to PATH." {
		t.Errorf("Caveats = %q", meta.Caveats)
	}
}

func TestAddFormula_Edges_StructuredDeps(t *testing.T) {
	t.Parallel()
	f, err := formula.Parse([]byte(graphFixtureYAML))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	g := New()
	g.AddFormula(f)

	edges := g.Edges["mypkg"]
	if len(edges) != 3 {
		t.Fatalf("want 3 edges, got %d: %v", len(edges), edges)
	}
	want := []string{"libfoo", "cmake", "bats"}
	for i, name := range want {
		if edges[i] != name {
			t.Errorf("edges[%d] = %q, want %q", i, edges[i], name)
		}
	}
}

func TestAddFormula_Edges_LegacyFallback(t *testing.T) {
	t.Parallel()
	f, err := formula.Parse([]byte(`
name: legacy
version: "1.0"
description: "test"
homepage: "https://example.com"
license: "MIT"
url:
  darwin_arm64: "https://example.com/legacy"
sha256:
  darwin_arm64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
install:
  type: binary
  binary_name: legacy
dependencies:
  - dep1
  - dep2
`))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	g := New()
	g.AddFormula(f)

	edges := g.Edges["legacy"]
	if len(edges) != 2 || edges[0] != "dep1" || edges[1] != "dep2" {
		t.Errorf("unexpected legacy edges: %v", edges)
	}
}

func TestNew_Empty(t *testing.T) {
	t.Parallel()
	g := New()
	if g.Edges == nil {
		t.Error("Edges map is nil")
	}
	if g.Meta == nil {
		t.Error("Meta map is nil")
	}
}

func TestAddNode_AddEdge(t *testing.T) {
	t.Parallel()
	g := New()
	g.AddNode(NodeMeta{Name: "a"})
	g.AddNode(NodeMeta{Name: "b"})

	if err := g.AddEdge("a", "b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Edges["a"]) != 1 || g.Edges["a"][0] != "b" {
		t.Errorf("edges[a] = %v, want [b]", g.Edges["a"])
	}

	if err := g.AddEdge("unknown", "b"); err == nil {
		t.Error("expected error for unknown from-node")
	}
}

func TestRemoveNode(t *testing.T) {
	t.Parallel()
	g := New()
	g.AddNode(NodeMeta{Name: "a"})
	g.AddNode(NodeMeta{Name: "b"})
	g.AddNode(NodeMeta{Name: "c"})
	_ = g.AddEdge("a", "b")
	_ = g.AddEdge("a", "c")
	_ = g.AddEdge("b", "c")

	g.RemoveNode("c")

	if _, ok := g.Meta["c"]; ok {
		t.Error("Meta[c] should be gone")
	}
	if _, ok := g.Edges["c"]; ok {
		t.Error("Edges[c] should be gone")
	}
	for _, dep := range g.Edges["a"] {
		if dep == "c" {
			t.Error("c still present in a's edges")
		}
	}
	for _, dep := range g.Edges["b"] {
		if dep == "c" {
			t.Error("c still present in b's edges")
		}
	}
}

func TestDetectCycles_Acyclic(t *testing.T) {
	t.Parallel()
	g := New()
	g.AddNode(NodeMeta{Name: "a"})
	g.AddNode(NodeMeta{Name: "b"})
	g.AddNode(NodeMeta{Name: "c"})
	_ = g.AddEdge("a", "b")
	_ = g.AddEdge("b", "c")

	if cycles := g.DetectCycles(); cycles != nil {
		t.Errorf("expected nil cycles, got %v", cycles)
	}
}

func TestDetectCycles_Cycle(t *testing.T) {
	t.Parallel()
	g := New()
	g.AddNode(NodeMeta{Name: "a"})
	g.AddNode(NodeMeta{Name: "b"})
	_ = g.AddEdge("a", "b")
	_ = g.AddEdge("b", "a")

	cycles := g.DetectCycles()
	if len(cycles) == 0 {
		t.Fatal("expected at least one cycle")
	}
	cycle := cycles[0]
	if len(cycle) < 2 {
		t.Fatalf("cycle too short: %v", cycle)
	}
	names := make(map[string]bool)
	for _, n := range cycle {
		names[n] = true
	}
	if !names["a"] || !names["b"] {
		t.Errorf("cycle should contain a and b, got %v", cycle)
	}
}

func TestDetectCycles_Empty(t *testing.T) {
	t.Parallel()
	g := New()
	if cycles := g.DetectCycles(); cycles != nil {
		t.Errorf("empty graph should return nil, got %v", cycles)
	}
}

func TestTopologicalSort_Linear(t *testing.T) {
	t.Parallel()
	g := New()
	g.AddNode(NodeMeta{Name: "a"})
	g.AddNode(NodeMeta{Name: "b"})
	g.AddNode(NodeMeta{Name: "c"})
	_ = g.AddEdge("a", "b")
	_ = g.AddEdge("b", "c")

	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	order := indexMap(sorted)
	if order["c"] >= order["b"] {
		t.Errorf("c (pos %d) should precede b (pos %d)", order["c"], order["b"])
	}
	if order["b"] >= order["a"] {
		t.Errorf("b (pos %d) should precede a (pos %d)", order["b"], order["a"])
	}
}

func TestTopologicalSort_Diamond(t *testing.T) {
	t.Parallel()
	g := New()
	for _, n := range []string{"a", "b", "c", "d"} {
		g.AddNode(NodeMeta{Name: n})
	}
	_ = g.AddEdge("a", "b")
	_ = g.AddEdge("a", "c")
	_ = g.AddEdge("b", "d")
	_ = g.AddEdge("c", "d")

	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sorted) != 4 {
		t.Fatalf("want 4 nodes, got %d: %v", len(sorted), sorted)
	}
	order := indexMap(sorted)
	if order["d"] >= order["b"] || order["d"] >= order["c"] {
		t.Errorf("d should precede b and c, order: %v", order)
	}
}

func TestTopologicalSort_RuntimeBeforeBuild(t *testing.T) {
	t.Parallel()
	g := New()
	// runtime and build have no mutual dependency — both are leaves at the same level.
	g.AddNode(NodeMeta{Name: "runtime", Kind: formula.DepRuntime})
	g.AddNode(NodeMeta{Name: "build", Kind: formula.DepBuild})
	g.AddNode(NodeMeta{Name: "top"})
	_ = g.AddEdge("top", "runtime")
	_ = g.AddEdge("top", "build")

	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	order := indexMap(sorted)
	if order["runtime"] >= order["build"] {
		t.Errorf("runtime (pos %d) should precede build (pos %d)", order["runtime"], order["build"])
	}
}

func TestTopologicalSort_Cycle(t *testing.T) {
	t.Parallel()
	g := New()
	g.AddNode(NodeMeta{Name: "x"})
	g.AddNode(NodeMeta{Name: "y"})
	_ = g.AddEdge("x", "y")
	_ = g.AddEdge("y", "x")

	_, err := g.TopologicalSort()
	if !errors.Is(err, ErrCycleDetected) {
		t.Errorf("want ErrCycleDetected, got %v", err)
	}
}

// indexMap returns a name→position map for a sorted slice.
func indexMap(names []string) map[string]int {
	m := make(map[string]int, len(names))
	for i, n := range names {
		m[n] = i
	}
	return m
}
