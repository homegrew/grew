package depgraph

import (
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

	g := NewGraph()
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

	g := NewGraph()
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

	g := NewGraph()
	g.AddFormula(f)

	edges := g.Edges["legacy"]
	if len(edges) != 2 || edges[0] != "dep1" || edges[1] != "dep2" {
		t.Errorf("unexpected legacy edges: %v", edges)
	}
}
