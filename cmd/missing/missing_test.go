package missing

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/pkg/config"
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

func installFormula(t *testing.T, cellarDir, name string) {
	t.Helper()
	keg := filepath.Join(cellarDir, name, "1.0")
	if err := os.MkdirAll(keg, 0755); err != nil {
		t.Fatal(err)
	}
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

// setupPrefix builds a mock prefix with a core tap and returns its Paths.
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
	// Reset the package-level flag so tests don't leak into each other.
	missingHide = ""
	t.Cleanup(func() { missingHide = "" })
	return paths
}

// TestRunMissingNoMissing: all runtime deps installed, expect clean output and no error.
func TestRunMissingNoMissing(t *testing.T) {
	paths := setupPrefix(t)
	tap, cellar := paths.CoreTap, paths.Cellar

	writeFormula(t, tap, "dep1", nil)
	writeFormula(t, tap, "pkga", []string{"dep1"})
	installFormula(t, cellar, "dep1")
	installFormula(t, cellar, "pkga")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runMissing(nil)
	})
	if runErr != nil {
		t.Fatalf("expected no error, got %v", runErr)
	}
	if out != "" {
		t.Fatalf("expected no output, got %q", out)
	}
}

// TestRunMissingDetectsMissing: a dependency keg is absent, expect a report and a non-zero error.
func TestRunMissingDetectsMissing(t *testing.T) {
	paths := setupPrefix(t)
	tap, cellar := paths.CoreTap, paths.Cellar

	writeFormula(t, tap, "dep1", nil)
	writeFormula(t, tap, "pkga", []string{"dep1"})
	// Only pkga is installed; dep1 is declared but not present in the cellar.
	installFormula(t, cellar, "pkga")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runMissing(nil)
	})
	if runErr == nil {
		t.Fatal("expected a non-nil error when a dependency is missing")
	}
	if out != "pkga: dep1\n" {
		t.Fatalf("expected %q, got %q", "pkga: dep1\n", out)
	}
}

// TestRunMissingSpecificTarget: only the named formula is checked.
func TestRunMissingSpecificTarget(t *testing.T) {
	paths := setupPrefix(t)
	tap, cellar := paths.CoreTap, paths.Cellar

	writeFormula(t, tap, "dep1", nil)
	writeFormula(t, tap, "pkga", []string{"dep1"})
	writeFormula(t, tap, "pkgb", nil)
	installFormula(t, cellar, "pkga")
	installFormula(t, cellar, "pkgb")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runMissing([]string{"pkgb"})
	})
	if runErr != nil {
		t.Fatalf("expected no error checking pkgb, got %v", runErr)
	}
	if out != "" {
		t.Fatalf("expected no output for pkgb, got %q", out)
	}
}

// TestRunMissingHideFlag: --hide makes an installed dependency appear absent.
func TestRunMissingHideFlag(t *testing.T) {
	paths := setupPrefix(t)
	tap, cellar := paths.CoreTap, paths.Cellar

	writeFormula(t, tap, "dep1", nil)
	writeFormula(t, tap, "pkga", []string{"dep1"})
	installFormula(t, cellar, "dep1")
	installFormula(t, cellar, "pkga")

	missingHide = "dep1"

	var runErr error
	out := captureStdout(t, func() {
		runErr = runMissing([]string{"pkga"})
	})
	if runErr == nil {
		t.Fatal("expected a non-nil error when --hide hides an installed dependency")
	}
	if out != "pkga: dep1\n" {
		t.Fatalf("expected %q, got %q", "pkga: dep1\n", out)
	}
}
