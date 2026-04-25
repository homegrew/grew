//go:build integration

package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveEndToEnd executes the full E2E suite against real remote taps
// and live GitHub endpoints. It exactly mirrors the steps defined in
// .github/workflows/e2e.yml.
//
// Because this test touches the real network, clones git repositories,
// and compiles software from source (e.g., jq), it can take several minutes.
// It can be skipped by running `go test -short`.
func TestLiveEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live E2E test in short mode")
	}

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("failed to eval symlinks: %v", err)
	}

	// Ensure the temp directory can be cleaned up. `go build` during setup
	// downloads modules to $HOME/go/pkg/mod, making them read-only.
	t.Cleanup(func() {
		filepath.WalkDir(tmpDir, func(path string, d os.DirEntry, err error) error {
			if err == nil {
				info, err := d.Info()
				if err == nil {
					os.Chmod(path, info.Mode()|0200)
				}
			}
			return nil
		})
	})

	prefix := filepath.Join(tmpDir, "homegrew")
	exePath := filepath.Join(tmpDir, "grew")

	// 1. Build the real grew binary
	t.Log("Building live grew binary...")
	root := getProjectRoot(t)
	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", exePath, filepath.Join(root, "main.go"))
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build real binary: %v\nOutput:\n%s", err, string(out))
	}

	// Route all grew operations to our isolated temporary prefix
	env := append(os.Environ(), "HOMEGREW_PREFIX="+prefix, "HOME="+tmpDir)
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		if val := os.Getenv(key); val != "" {
			env = append(env, key+"="+val)
		}
	}

	runCmd := func(args ...string) string {
		t.Helper()
		t.Logf("=> grew %s", strings.Join(args, " "))
		cmd := exec.Command(exePath, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Command 'grew %s' failed: %v\nOutput:\n%s", strings.Join(args, " "), err, string(out))
		}
		return string(out)
	}

	// 2. Setup (User-local)
	runCmd("setup", "--unsafe")

	// 3. Update (Fetches real taps from GitHub)
	runCmd("update")

	// 4. Install nano
	runCmd("install", "-v", "-d", "nano")

	nanoBin := filepath.Join(prefix, "bin", "nano")
	if _, err := os.Stat(nanoBin); err != nil {
		t.Fatalf("nano binary not found in bin/: %v", err)
	}

	// Verify the installed binary is executable
	checkNano := exec.Command(nanoBin, "-V")
	if out, err := checkNano.CombinedOutput(); err != nil {
		t.Fatalf("failed to execute installed nano: %v\nOutput: %s", err, string(out))
	}

	// 5. Test Dependency Resolution
	outDeps := runCmd("deps", "--tree", "nano")
	if !strings.Contains(outDeps, "nano") {
		t.Errorf("deps output missing 'nano', got output: %q", outDeps)
	}

	// 6. Test Link and Unlink
	runCmd("unlink", "nano")
	if _, err := os.Stat(nanoBin); err == nil {
		t.Fatalf("nano should have been unlinked from bin/")
	}

	runCmd("link", "nano")
	if _, err := os.Stat(nanoBin); err != nil {
		t.Fatalf("nano should have been relinked into bin/")
	}

	// 7. Test Build from Source (Sandbox & Relocation)
	// This mirrors the CI step to ensure from-source compilation works.
	runCmd("install", "-s", "xz")
	xzBin := filepath.Join(prefix, "bin", "xz")
	if _, err := os.Stat(xzBin); err != nil {
		t.Fatalf("xz binary not found after source build: %v", err)
	}

	checkXz := exec.Command(xzBin, "--version")
	if out, err := checkXz.CombinedOutput(); err != nil {
		t.Fatalf("failed to execute built xz: %v\nOutput: %s", err, string(out))
	}
	// 8. Test Reinstall (Offline/Cache verification)
	runCmd("reinstall", "nano")

	// 9. Test Manifest Verification
	runCmd("verify", "nano")

	// 10. Test Vulnerability Audit
	t.Log("=> grew vuln-scan")
	cmdVuln := exec.Command(exePath, "vuln-scan")
	cmdVuln.Env = env
	outVuln, errVuln := cmdVuln.CombinedOutput()
	// We intentionally do not fail the test on a non-zero exit code here,
	// as a legitimate CVE might exist in upstream packages at any given time.
	t.Logf("vuln-scan output (err=%v):\n%s", errVuln, string(outVuln))

	// 11. Test Lockfile Generation
	runCmd("lock", "generate")
	runCmd("lock", "check")
	if _, err := os.Stat(filepath.Join(prefix, "grew.lock")); err != nil {
		t.Fatalf("grew.lock not generated")
	}

	// 12. Test Shellenv Generation
	outEnv := runCmd("shellenv", "bash")
	if !strings.Contains(outEnv, "export HOMEGREW_PREFIX") {
		t.Errorf("shellenv output missing export statement")
	}
}
