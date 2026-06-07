package uses

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

func TestRunUsesDirectDependents(t *testing.T) {
	// no t.Parallel: these tests temporarily redirect os.Stdout
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

	coreTap := paths.CoreTap
	if err := os.MkdirAll(coreTap, 0755); err != nil {
		t.Fatal(err)
	}

	writeFormula(t, coreTap, "openssl", nil)
	writeFormula(t, coreTap, "git", []string{"openssl"})
	writeFormula(t, coreTap, "curl", []string{"openssl"})
	writeFormula(t, coreTap, "xz", nil)

	installFormula(t, paths.Cellar, "git")
	installFormula(t, paths.Cellar, "curl")
	installFormula(t, paths.Cellar, "xz")

	out := captureStdout(t, func() {
		if err := runUses([]string{"openssl"}); err != nil {
			t.Fatalf("runUses returned error: %v", err)
		}
	})

	lines := strings.Fields(strings.TrimSpace(out))
	got := strings.Join(lines, " ")
	if got != "curl git" {
		t.Fatalf("expected 'curl git', got %q", got)
	}
}

func TestRunUsesNoDependents(t *testing.T) {
	// no t.Parallel: these tests temporarily redirect os.Stdout
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

	coreTap := paths.CoreTap
	if err := os.MkdirAll(coreTap, 0755); err != nil {
		t.Fatal(err)
	}

	writeFormula(t, coreTap, "openssl", nil)
	writeFormula(t, coreTap, "xz", nil)
	installFormula(t, paths.Cellar, "xz")

	out := captureStdout(t, func() {
		if err := runUses([]string{"openssl"}); err != nil {
			t.Fatalf("runUses returned error: %v", err)
		}
	})

	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected no output, got %q", out)
	}
}
