//go:build integration

package tests

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDependencyCircular(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	// Create circular dependency: a -> b -> a
	createFormula(t, prefix, "a", `name: a
version: 1.0.0
dependencies: [b]
url:
  darwin_arm64: https://example.com/a.tar.gz
install:
  type: archive
`)
	createFormula(t, prefix, "b", `name: b
version: 1.0.0
dependencies: [a]
url:
  darwin_arm64: https://example.com/b.tar.gz
install:
  type: archive
`)

	cmd := exec.Command(exePath, "install", "a")
	cmd.Env = append(os.Environ(), "HOMEGREW_PREFIX="+prefix)
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("expected error for circular dependency, but got none")
	}
	if !strings.Contains(string(out), "circular dependency") {
		t.Errorf("expected circular dependency error message, got: %s", string(out))
	}
}

func TestDependencyMissing(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	createFormula(t, prefix, "mainpkg", `name: mainpkg
version: 1.0.0
dependencies: [nonexistent]
url:
  darwin_arm64: https://example.com/mainpkg.tar.gz
install:
  type: archive
`)

	cmd := exec.Command(exePath, "install", "mainpkg")
	cmd.Env = append(os.Environ(), "HOMEGREW_PREFIX="+prefix)
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Error("expected error for missing dependency, but got none")
	}
	if !strings.Contains(string(out), "not found") {
		t.Errorf("expected 'not found' error message, got: %s", string(out))
	}
}
