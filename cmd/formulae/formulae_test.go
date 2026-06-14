package formulae

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/formula"
)

func setupPrefix(t *testing.T) config.Paths {
	t.Helper()
	tmpDir := t.TempDir()
	paths := config.FromRoot(
		filepath.Join(tmpDir, "root"),
		filepath.Join(tmpDir, "Applications"),
		filepath.Join(tmpDir, "cache"),
	)
	if err := paths.Init(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOMEGREW_NO_INIT_TAP", "1")
	t.Setenv("HOMEGREW_PREFIX", paths.Root)
	if err := os.MkdirAll(paths.CoreTap, 0755); err != nil {
		t.Fatal(err)
	}
	return paths
}

func writeFormula(t *testing.T, formulaDir, name, description string) {
	t.Helper()
	yaml := `name: ` + name + `
version: 1.0.0
description: ` + description + `
homepage: https://example.com
license: MIT
url:
  darwin_arm64: https://example.com/` + name + `.tar.gz
sha256:
  darwin_arm64: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
install:
  type: binary
  binary_name: ` + name + `
`
	if err := os.WriteFile(filepath.Join(formulaDir, name+".yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

func newContext(t *testing.T) *context.Context {
	t.Helper()
	ctx, err := context.New()
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
		_ = w.Close()
		_ = r.Close()
	}()
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestRunFormulae_Empty(t *testing.T) {
	paths := setupPrefix(t)
	formulaDir := filepath.Join(paths.CoreTap, "Formula")
	if err := os.MkdirAll(formulaDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx := newContext(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runFormulae(ctx)
	})
	if runErr != nil {
		t.Fatalf("runFormulae: %v", runErr)
	}
	if !strings.Contains(out, "No formulae available") {
		t.Errorf("expected 'No formulae available' message, got: %q", out)
	}
}

func TestRunFormulae_SingleFormula(t *testing.T) {
	paths := setupPrefix(t)
	formulaDir := filepath.Join(paths.CoreTap, "Formula")
	if err := os.MkdirAll(formulaDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeFormula(t, formulaDir, "jq", "A lightweight JSON processor")

	ctx := newContext(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runFormulae(ctx)
	})
	if runErr != nil {
		t.Fatalf("runFormulae: %v", runErr)
	}
	if !strings.Contains(out, "jq") || !strings.Contains(out, "A lightweight JSON processor") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunFormulae_SortedOutput(t *testing.T) {
	paths := setupPrefix(t)
	formulaDir := filepath.Join(paths.CoreTap, "Formula")
	if err := os.MkdirAll(formulaDir, 0755); err != nil {
		t.Fatal(err)
	}

	writeFormula(t, formulaDir, "zzz", "Last package")
	writeFormula(t, formulaDir, "aaa", "First package")
	writeFormula(t, formulaDir, "mmm", "Middle package")

	ctx := newContext(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = runFormulae(ctx)
	})
	if runErr != nil {
		t.Fatalf("runFormulae: %v", runErr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "aaa") {
		t.Errorf("first line should start with 'aaa', got: %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "zzz") {
		t.Errorf("last line should start with 'zzz', got: %q", lines[2])
	}
}

func TestDedupe_Formulae(t *testing.T) {
	f1 := &formula.Formula{Name: "foo", Description: "first"}
	f2 := &formula.Formula{Name: "foo", Description: "second"}
	f3 := &formula.Formula{Name: "bar", Description: "baz"}

	input := []*formula.Formula{f1, f2, f3}
	result := dedupe(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 formulae after dedup, got %d", len(result))
	}
	if result[0].Name != "foo" || result[0].Description != "first" {
		t.Errorf("first formula should be foo/first, got %s/%s", result[0].Name, result[0].Description)
	}
	if result[1].Name != "bar" {
		t.Errorf("second formula should be bar, got %s", result[1].Name)
	}
}
