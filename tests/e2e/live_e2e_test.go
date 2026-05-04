package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/tests/testhelper"
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
					if chmodErr := os.Chmod(path, info.Mode()|0200); chmodErr != nil {
						t.Logf("cleanup: failed to chmod %q: %v", path, chmodErr)
					}
				}
			}
			return nil
		})
	})

	prefix := filepath.Join(tmpDir, "homegrew")
	exePath := filepath.Join(tmpDir, "grew")

	// 1. Build the real grew binary
	t.Log("Building live grew binary...")
	root := testhelper.GetProjectRoot(t)
	args := []string{"build", "-tags=devmode", "-o", exePath, "."}

	cmdBuild := exec.Command("go", args...)
	cmdBuild.Dir = root
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
	// `--unsafe` is required here to allow non-interactive setup in this live E2E run.
	// This test is isolated to a temporary HOME/HOMEGREW_PREFIX and mirrors CI behavior.
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

	// 8.1 Test install --force
	runCmd("install", "--force", "nano")

	// 8.2 Test uninstall --force
	runCmd("uninstall", "--force", "nano")
	if _, err := os.Stat(nanoBin); err == nil {
		t.Fatalf("nano should have been uninstalled")
	}

	// 8.3 Test reinstall --force
	// reinstall --force should succeed even though nano is not currently installed
	runCmd("reinstall", "--force", "nano")
	if _, err := os.Stat(nanoBin); err != nil {
		t.Fatalf("nano binary not found after reinstall --force: %v", err)
	}

	// 9. Test Manifest Verification
	runCmd("verify", "nano")
	runCmd("install", "coreutils")

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

	outList := runCmd("list")
	t.Log("=> grew list\n", string(outList))

	// 12. Test Shellenv Generation
	outEnv := runCmd("shellenv", "bash")
	for _, v := range []string{"HOMEGREW_PREFIX", "HOMEGREW_CELLAR", "HOMEGREW_REPOSITORY", "MANPATH", "INFOPATH"} {
		if !strings.Contains(outEnv, "export "+v) {
			t.Errorf("shellenv output missing export statement for %s, output:\n%s", v, outEnv)
		}
	}

	// 13. Test Leaves command
	outLeaves := runCmd("leaves")
	if !strings.Contains(outLeaves, "nano") {
		t.Errorf("leaves output missing 'nano', got output: %q", outLeaves)
	}
	if !strings.Contains(outLeaves, "xz") {
		t.Errorf("leaves output missing 'xz', got output: %q", outLeaves)
	}

	// 14. Test Bash (Relocation Verification)
	// Bash is a good test for text-file relocation as it has hardcoded paths in scripts.
	runCmd("install", "bash")
	bashBin := filepath.Join(prefix, "bin", "bash")
	if _, err := os.Stat(bashBin); err != nil {
		t.Fatalf("bash binary not found: %v", err)
	}
	// Run bash to verify it works
	checkBash := exec.Command(bashBin, "-c", "echo relocation-success")
	checkBash.Env = env
	out, err := checkBash.CombinedOutput()
	if err != nil {
		t.Fatalf("bash relocation verification failed: %v\nOutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "relocation-success") {
		t.Errorf("bash output missing 'relocation-success', got: %q", string(out))
	}
}
