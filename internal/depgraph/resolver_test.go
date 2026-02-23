package depgraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/formula"
)

func writeFormula(t *testing.T, dir, name string, deps []string) {
	t.Helper()
	yaml := "name: " + name + "\nversion: \"1.0\"\ndescription: \"test\"\nhomepage: \"https://example.com\"\nlicense: \"MIT\"\nurl:\n  darwin_arm64: \"https://example.com/" + name + "\"\n  linux_amd64: \"https://example.com/" + name + "\"\nsha256:\n  darwin_arm64: \"abc\"\n  linux_amd64: \"def\"\ninstall:\n  type: binary\n  binary_name: " + name + "\ndependencies:\n"
	if len(deps) == 0 {
		yaml += "  []\n"
	} else {
		for _, d := range deps {
			yaml += "  - " + d + "\n"
		}
	}
	yaml += "keg_only: false\n"
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_NoDeps(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)
	writeFormula(t, tapDir, "solo", nil)

	loader := &formula.Loader{TapDir: tmpDir}
	resolver := &Resolver{Loader: loader}

	result, err := resolver.Resolve("solo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 formula, got %d", len(result))
	}
	if result[0].Name != "solo" {
		t.Errorf("expected 'solo', got %q", result[0].Name)
	}
}

func TestResolve_LinearChain(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)
	writeFormula(t, tapDir, "a", []string{"b"})
	writeFormula(t, tapDir, "b", []string{"c"})
	writeFormula(t, tapDir, "c", nil)

	loader := &formula.Loader{TapDir: tmpDir}
	resolver := &Resolver{Loader: loader}

	result, err := resolver.Resolve("a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 formulas, got %d", len(result))
	}
	// c must come before b, b before a
	order := map[string]int{}
	for i, f := range result {
		order[f.Name] = i
	}
	if order["c"] >= order["b"] {
		t.Errorf("c (pos %d) should come before b (pos %d)", order["c"], order["b"])
	}
	if order["b"] >= order["a"] {
		t.Errorf("b (pos %d) should come before a (pos %d)", order["b"], order["a"])
	}
}

func TestResolve_Diamond(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)
	writeFormula(t, tapDir, "a", []string{"b", "c"})
	writeFormula(t, tapDir, "b", []string{"d"})
	writeFormula(t, tapDir, "c", []string{"d"})
	writeFormula(t, tapDir, "d", nil)

	loader := &formula.Loader{TapDir: tmpDir}
	resolver := &Resolver{Loader: loader}

	result, err := resolver.Resolve("a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 formulas, got %d", len(result))
	}
	order := map[string]int{}
	for i, f := range result {
		order[f.Name] = i
	}
	if order["d"] >= order["b"] {
		t.Errorf("d should come before b")
	}
	if order["d"] >= order["c"] {
		t.Errorf("d should come before c")
	}
	if order["b"] >= order["a"] {
		t.Errorf("b should come before a")
	}
	if order["c"] >= order["a"] {
		t.Errorf("c should come before a")
	}
}

func TestResolve_CircularDependency(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)
	writeFormula(t, tapDir, "x", []string{"y"})
	writeFormula(t, tapDir, "y", []string{"x"})

	loader := &formula.Loader{TapDir: tmpDir}
	resolver := &Resolver{Loader: loader}

	_, err := resolver.Resolve("x")
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if _, ok := err.(*CycleError); !ok {
		t.Errorf("expected *CycleError, got %T: %v", err, err)
	}
}

func TestResolve_MissingDependency(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)
	writeFormula(t, tapDir, "a", []string{"missing"})

	loader := &formula.Loader{TapDir: tmpDir}
	resolver := &Resolver{Loader: loader}

	_, err := resolver.Resolve("a")
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}
