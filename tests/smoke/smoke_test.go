package smoke

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/pkg/testhelper"
)

func TestSmoke_Version(t *testing.T) {
	t.Parallel()
	_, exePath, env := setupBinary(t)

	cmd := exec.Command(exePath, "version")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\nOutput: %s", err, string(out))
	}
	if len(out) == 0 {
		t.Errorf("expected version output, got empty")
	}
}

func TestSmoke_Help(t *testing.T) {
	t.Parallel()
	_, exePath, env := setupBinary(t)

	cmd := exec.Command(exePath, "help")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help command failed: %v\nOutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Usage:") {
		t.Errorf("expected usage output, got: %s", string(out))
	}
}

func TestSmoke_Config(t *testing.T) {
	t.Parallel()
	_, exePath, env := setupBinary(t)

	cmd := exec.Command(exePath, "config")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("config command failed: %v\nOutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "HOMEGREW_PREFIX") {
		t.Errorf("expected HOMEGREW_PREFIX in config output, got: %s", string(out))
	}
}

func TestSmoke_List(t *testing.T) {
	t.Parallel()
	_, exePath, env := setupBinary(t)

	cmd := exec.Command(exePath, "list")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list command failed: %v\nOutput: %s", err, string(out))
	}
}

func TestSmoke_Doctor(t *testing.T) {
	t.Parallel()
	_, exePath, env := setupBinary(t)

	cmd := exec.Command(exePath, "dr")
	cmd.Env = env
	// Doctor might exit with an error if warnings exist (like missing PATH, empty tap).
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Logf("Doctor command passed cleanly: %s", string(out))
	} else {
		t.Logf("Doctor command reported warnings (expected in fresh env): %v\nOutput: %s", err, string(out))
	}
}

func TestSmoke_Search(t *testing.T) {
	t.Parallel()
	prefix, exePath, env := setupBinary(t)

	// Create a dummy formula to search for
	testhelper.CreateFormula(t, prefix, "smokepkg", `name: smokepkg
version: 1.0.0
description: A package for testing
`)

	cmd := exec.Command(exePath, "search", "smokepkg")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("search command failed: %v\nOutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "smokepkg") {
		t.Errorf("expected search output to contain 'smokepkg', got: %s", string(out))
	}
}

func TestSmoke_Info(t *testing.T) {
	t.Parallel()
	prefix, exePath, env := setupBinary(t)

	// Create a dummy formula to get info for
	testhelper.CreateFormula(t, prefix, "infopkg", `name: infopkg
version: 2.0.0
description: A package for testing info
url:
  darwin_arm64: https://example.com/infopkg.tar.gz
install:
  type: archive
`)

	cmd := exec.Command(exePath, "info", "infopkg")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("info command failed: %v\nOutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "A package for testing info") {
		t.Errorf("expected info output to contain description, got: %s", string(out))
	}
}

func TestSmoke_Deps(t *testing.T) {
	t.Parallel()
	prefix, exePath, env := setupBinary(t)

	testhelper.CreateFormula(t, prefix, "depa", `name: depa
version: 1.0.0
url:
  darwin_arm64: https://example.com/depa.tar.gz
install:
  type: archive
`)

	testhelper.CreateFormula(t, prefix, "pkgb", `name: pkgb
version: 1.0.0
dependencies: [depa]
url:
  darwin_arm64: https://example.com/pkgb.tar.gz
install:
  type: archive
`)

	cmd := exec.Command(exePath, "deps", "pkgb")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("deps command failed: %v\nOutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "depa") {
		t.Errorf("expected deps output to contain 'depa', got: %s", string(out))
	}
}

func TestSmoke_DoctorQuiet(t *testing.T) {
	t.Parallel()
	_, exePath, env := setupBinary(t)

	// Test local quiet flag
	cmd := exec.Command(exePath, "dr", "-q")
	cmd.Env = env
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "Checking grew installation...") {
		t.Errorf("Expected quiet output to omit 'Checking grew installation...', got: %s", string(out))
	}

	// Test global quiet flag
	cmdGlobal := exec.Command(exePath, "-q", "dr")
	cmdGlobal.Env = env
	outGlobal, _ := cmdGlobal.CombinedOutput()
	if strings.Contains(string(outGlobal), "Checking grew installation...") {
		t.Errorf("Expected global quiet output to omit 'Checking grew installation...', got: %s", string(outGlobal))
	}
}

func setupBinary(t *testing.T) (string, string, []string) {
	t.Helper()
	tmpDir := t.TempDir()
	prefix := testhelper.SetupPrefix(t, tmpDir)
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	env := append(os.Environ(), "HOMEGREW_PREFIX="+prefix, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	return prefix, exePath, env
}
