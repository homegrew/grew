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

	// Create circular dependency: A -> B -> A
	createFormula(t, prefix, "A", `name: A
version: 1.0.0
dependencies: [B]
install:
  type: archive
`)
	createFormula(t, prefix, "B", `name: B
version: 1.0.0
dependencies: [A]
install:
  type: archive
`)

	cmd := exec.Command(exePath, "install", "A")
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
