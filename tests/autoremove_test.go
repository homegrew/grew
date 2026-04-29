package tests

import (
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

	exePath := buildTestBinary(t, tmpDir)
	prefix := setupPrefix(t, tmpDir)

	archiveData := []byte("fake-binary-data")
	archiveSHA := computeSHA256(archiveData)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveData)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)

	createFormula(t, prefix, "base", `
name: base
version: 1.0.0
url:
  `+platformKey()+`: `+server.URL+`/base.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
`)

	createFormula(t, prefix, "dep1", `
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

	createFormula(t, prefix, "pkga", `
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
