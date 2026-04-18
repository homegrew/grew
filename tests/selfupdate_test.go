package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRunSelfUpdateIntegration tests the RunSelfUpdate function end-to-end.
// It compiles a dummy binary that invokes cmd.RunSelfUpdate(), places it in a
// mock grew prefix, and executes it. This simulates a real self-update where
// the binary replaces itself. Since there is no git repository in the mock prefix,
// it falls back to downloading the release from GitHub.
func TestRunSelfUpdateIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test that hits GitHub API in short mode")
	}

	tmpDir := t.TempDir()

	// Create prefix structure
	prefix := filepath.Join(tmpDir, "prefix")
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	exePath := filepath.Join(binDir, "grew")

	// Compile the dummy binary from testbin
	cmdBuild := exec.Command("go", "build", "-o", exePath, "./testbin/main.go")
	cmdBuild.Stdout = os.Stdout
	cmdBuild.Stderr = os.Stderr
	if err := cmdBuild.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}

	// Run the dummy binary
	cmdRun := exec.Command(exePath, "run")
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr
	cmdRun.Env = append(os.Environ(), "HOMEGREW_PREFIX="+prefix)
	
	// The dummy binary will run, detect its path, and download the real grew binary
	// from GitHub to replace itself.
	if err := cmdRun.Run(); err != nil {
		t.Fatalf("RunSelfUpdate failed: %v", err)
	}

	// After running, exePath should now be the real grew binary!
	cmdVerify := exec.Command(exePath, "--version")
	out, err := cmdVerify.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run replaced binary: %v, output: %s", err, string(out))
	}
	if len(out) == 0 {
		t.Errorf("expected version output, got empty")
	}
}

// TestSelfUpdateFromReleaseIntegration tests the SelfUpdateFromRelease function
// end-to-end by explicitly calling it.
func TestSelfUpdateFromReleaseIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test that hits GitHub API in short mode")
	}

	tmpDir := t.TempDir()

	// Create prefix structure
	prefix := filepath.Join(tmpDir, "prefix")
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	exePath := filepath.Join(binDir, "grew")

	// Compile the dummy binary
	cmdBuild := exec.Command("go", "build", "-o", exePath, "./testbin/main.go")
	cmdBuild.Stdout = os.Stdout
	cmdBuild.Stderr = os.Stderr
	if err := cmdBuild.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}

	// Run the dummy binary
	cmdRun := exec.Command(exePath, "from-release")
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr
	cmdRun.Env = append(os.Environ(), "HOMEGREW_PREFIX="+prefix)
	
	if err := cmdRun.Run(); err != nil {
		t.Fatalf("SelfUpdateFromRelease failed: %v", err)
	}

	// Verify the binary was replaced and works
	cmdVerify := exec.Command(exePath, "--version")
	out, err := cmdVerify.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run replaced binary: %v, output: %s", err, string(out))
	}
	if len(out) == 0 {
		t.Errorf("expected version output, got empty")
	}
}
