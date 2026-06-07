
package integration

import (
	"github.com/homegrew/grew/tests/testhelper"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoremove(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	archiveData := []byte("fake-binary-data")
	archiveSHA := testhelper.ComputeSHA256(archiveData)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveData)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	testhelper.WriteServerCert(server, certFile)

	testhelper.CreateFormula(t, prefix, "base", `
name: base
version: 1.0.0
url:
  `+platformKey()+`: `+server.URL+`/base.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
`)

	testhelper.CreateFormula(t, prefix, "dep1", `
name: dep1
version: 1.0.0
dependencies:
  - base
url:
  `+platformKey()+`: `+server.URL+`/dep1.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
`)

	testhelper.CreateFormula(t, prefix, "pkga", `
name: pkga
version: 1.0.0
dependencies:
  - dep1
url:
  `+platformKey()+`: `+server.URL+`/pkga.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
`)

	env := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(prefix, "cache"),
		"HOMEGREW_TEST_CERT_FILE="+certFile,
		"HOMEGREW_ALLOWED_HOSTS=127.0.0.1,localhost,example.com",
	)

	runGrew := func(args ...string) (string, error) {
		cmd := exec.Command(exePath, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// 1. Explicitly install "base"
	if out, err := runGrew("install", "base"); err != nil {
		t.Fatalf("failed to install base: %v\nOutput: %s", err, out)
	}

	// 2. Explicitly install "pkga" (this will automatically pull in "dep1" as a dependency)
	if out, err := runGrew("install", "pkga"); err != nil {
		t.Fatalf("failed to install pkga: %v\nOutput: %s", err, out)
	}

	// Verify all are installed
	out, err := runGrew("list")
	if err != nil {
		t.Fatalf("failed to list: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "base") || !strings.Contains(out, "dep1") || !strings.Contains(out, "pkga") {
		t.Fatalf("expected base, dep1, pkga to be installed. list output: %s", out)
	}

	// 3. Uninstall "pkga"
	if out, err := runGrew("uninstall", "pkga"); err != nil {
		t.Fatalf("failed to uninstall pkga: %v\nOutput: %s", err, out)
	}

	// 4. Test autoremove --dry-run
	out, err = runGrew("autoremove", "--dry-run")
	if err != nil {
		t.Fatalf("failed to autoremove --dry-run: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Would uninstall: dep1") {
		t.Fatalf("expected autoremove --dry-run to list dep1, got: %s", out)
	}
	if strings.Contains(out, "base") {
		t.Fatalf("expected autoremove --dry-run to NOT list base (explicitly installed), got: %s", out)
	}

	// 5. Run actual autoremove
	out, err = runGrew("autoremove")
	if err != nil {
		t.Fatalf("failed to autoremove: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Autoremoving 1 unneeded formulae:") || !strings.Contains(out, "dep1") {
		t.Fatalf("expected autoremove output to show dep1 removed, got: %s", out)
	}

	// Verify the final state
	out, err = runGrew("list")
	if err != nil {
		t.Fatalf("failed to list after autoremove: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "base") {
		t.Fatalf("expected base to still be installed")
	}
	if strings.Contains(out, "dep1") {
		t.Fatalf("expected dep1 to be uninstalled by autoremove")
	}
}

// TestAutoremoveTransitive verifies that autoremove removes an entire chain of
// orphaned dependencies (root → mid → leaf) in a single invocation, not just
// the first level.
func TestAutoremoveTransitive(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	archiveData := []byte("fake-binary-data")
	archiveSHA := testhelper.ComputeSHA256(archiveData)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveData)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	testhelper.WriteServerCert(server, certFile)

	// leaf: no deps, will be auto-installed
	testhelper.CreateFormula(t, prefix, "leaf", `
name: leaf
version: 1.0.0
url:
  `+platformKey()+`: `+server.URL+`/leaf.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
`)

	// mid: depends on leaf, will be auto-installed
	testhelper.CreateFormula(t, prefix, "mid", `
name: mid
version: 1.0.0
dependencies:
  - leaf
url:
  `+platformKey()+`: `+server.URL+`/mid.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
`)

	// root: depends on mid, installed on request
	testhelper.CreateFormula(t, prefix, "root", `
name: root
version: 1.0.0
dependencies:
  - mid
url:
  `+platformKey()+`: `+server.URL+`/root.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
`)

	env := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(prefix, "cache"),
		"HOMEGREW_TEST_CERT_FILE="+certFile,
		"HOMEGREW_ALLOWED_HOSTS=127.0.0.1,localhost,example.com",
	)

	runGrew := func(args ...string) (string, error) {
		cmd := exec.Command(exePath, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := runGrew("install", "root"); err != nil {
		t.Fatalf("failed to install root: %v\nOutput: %s", err, out)
	}

	out, err := runGrew("list")
	if err != nil {
		t.Fatalf("list failed: %v\nOutput: %s", err, out)
	}
	for _, pkg := range []string{"root", "mid", "leaf"} {
		if !strings.Contains(out, pkg) {
			t.Fatalf("expected %s to be installed; list output: %s", pkg, out)
		}
	}

	if out, err := runGrew("uninstall", "root"); err != nil {
		t.Fatalf("failed to uninstall root: %v\nOutput: %s", err, out)
	}

	// A single autoremove must clean up the full chain (mid + leaf).
	out, err = runGrew("autoremove")
	if err != nil {
		t.Fatalf("autoremove failed: %v\nOutput: %s", err, out)
	}
	if !strings.Contains(out, "Autoremoving 2 unneeded formulae:") {
		t.Fatalf("expected autoremove to remove 2 formulae in one run, got: %s", out)
	}

	out, err = runGrew("list")
	if err != nil {
		t.Fatalf("list failed: %v\nOutput: %s", err, out)
	}
	for _, pkg := range []string{"root", "mid", "leaf"} {
		if strings.Contains(out, pkg) {
			t.Fatalf("expected %s to be uninstalled, but it still appears in list: %s", pkg, out)
		}
	}
}
