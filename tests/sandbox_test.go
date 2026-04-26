//go:build integration

package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/sandbox"
)

func TestSandboxEscape(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Sandbox tests (bubblewrap) only supported on Linux")
	}

	if !sandbox.IsSandboxed() {
		t.Skip("Functional sandboxing is not supported on this system")
	}

	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	// Attempt to write to a path outside the sandbox during extraction
	// We'll create a tarball that has a symlink to /tmp/evil and then tries to write through it.
	// Actually, grew's extractor might prevent absolute symlinks or escaping ones.
	// Let's try a post_install script that attempts to touch /tmp/sandbox_escape_test

	tarballBytes := makeDummyTarGz(t, "echo hello")
	tarballHash := computeSHA256(tarballBytes)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarballBytes)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	createFormula(t, prefix, "malicious", fmt.Sprintf(`name: malicious
version: 1.0.0
bottle:
  linux_amd64:
    url: %s/malicious.tar.gz
    sha256: %s
install:
  type: archive
post_install: |
  echo "SANDBOX_MARKER_RUN"
  touch /tmp/sandbox_escape_test
`, server.URL, tarballHash))

	cmd := exec.Command(exePath, "install", "malicious")
	cmd.Env = append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	// Ensure markers don't exist before test
	os.Remove("/tmp/sandbox_escape_test")

	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("install command failed: %v\nOutput:\n%s", err, string(out))
	}

	if !strings.Contains(string(out), "SANDBOX_MARKER_RUN") {
		t.Fatalf("post_install output not found, script did not run.\nOutput:\n%s", string(out))
	}

	if _, statErr := os.Stat("/tmp/sandbox_escape_test"); statErr == nil {
		t.Error("Sandbox escape successful! Post-install script wrote to /tmp")
		os.Remove("/tmp/sandbox_escape_test")
	} else if !os.IsNotExist(statErr) {
		t.Errorf("Unexpected error stating /tmp/sandbox_escape_test: %v", statErr)
	} else {
		t.Logf("Sandbox escape blocked as expected.")
	}
}
