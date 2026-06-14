package casks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/pkg/cask"
	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/context"
)

const minimalCaskYAML = `name: %s
version: 1.0.0
description: %s
url:
  darwin_arm64: https://example.com/%s.zip
sha256:
  darwin_arm64: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
artifacts:
  app:
    - Dummy.app
`

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

func writeCask(t *testing.T, caskDir, name, description string) {
	t.Helper()
	yaml := minimalCaskYAML
	caskPath := filepath.Join(caskDir, name+".yaml")
	if err := os.WriteFile(caskPath, []byte(yaml), 0644); err != nil {
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

func TestRunCasks_Empty(t *testing.T) {
	paths := setupPrefix(t)
	caskDir := filepath.Join(paths.CoreTap, "Casks")
	if err := os.MkdirAll(caskDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx := newContext(t)
	out := captureStdout(t, func() {
		_ = runCasks(ctx)
	})
	if !strings.Contains(out, "No casks available") {
		t.Errorf("expected 'No casks available' message, got: %q", out)
	}
}

func TestRunCasks_SingleCask(t *testing.T) {
	paths := setupPrefix(t)
	caskDir := filepath.Join(paths.CoreTap, "Casks")
	if err := os.MkdirAll(caskDir, 0755); err != nil {
		t.Fatal(err)
	}

	caskYAML := `name: firefox
version: 1.0.0
description: A fast browser
url:
  darwin_arm64: https://example.com/firefox.zip
sha256:
  darwin_arm64: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
artifacts:
  app:
    - Firefox.app
`
	if err := os.WriteFile(filepath.Join(caskDir, "firefox.yaml"), []byte(caskYAML), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := newContext(t)
	out := captureStdout(t, func() {
		_ = runCasks(ctx)
	})
	if !strings.Contains(out, "firefox") || !strings.Contains(out, "A fast browser") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunCasks_SortedOutput(t *testing.T) {
	paths := setupPrefix(t)
	caskDir := filepath.Join(paths.CoreTap, "Casks")
	if err := os.MkdirAll(caskDir, 0755); err != nil {
		t.Fatal(err)
	}

	casks := map[string]string{
		"zoom":   "Video calls",
		"alfred": "Launcher",
		"iterm2": "Terminal",
	}

	for name, desc := range casks {
		yaml := `name: ` + name + `
version: 1.0.0
description: ` + desc + `
url:
  darwin_arm64: https://example.com/` + name + `.zip
sha256:
  darwin_arm64: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
artifacts:
  app:
    - Dummy.app
`
		if err := os.WriteFile(filepath.Join(caskDir, name+".yaml"), []byte(yaml), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := newContext(t)
	out := captureStdout(t, func() {
		_ = runCasks(ctx)
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "alfred") {
		t.Errorf("first line should start with 'alfred', got: %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "zoom") {
		t.Errorf("last line should start with 'zoom', got: %q", lines[2])
	}
}

func TestDedupe_Casks(t *testing.T) {
	c1 := &cask.Cask{Name: "foo", Description: "first"}
	c2 := &cask.Cask{Name: "foo", Description: "second"}
	c3 := &cask.Cask{Name: "bar", Description: "baz"}

	input := []*cask.Cask{c1, c2, c3}
	result := dedupe(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 casks after dedup, got %d", len(result))
	}
	if result[0].Name != "foo" || result[0].Description != "first" {
		t.Errorf("first cask should be foo/first, got %s/%s", result[0].Name, result[0].Description)
	}
	if result[1].Name != "bar" {
		t.Errorf("second cask should be bar, got %s", result[1].Name)
	}
}
