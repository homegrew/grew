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
)

func TestLinkUnlinkConflict(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	tarballA := makeDummyTarGz(t, "echo A")
	tarballB := makeDummyTarGz(t, "echo B")
	
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "pkgA") {
			w.Write(tarballA)
		} else {
			w.Write(tarballB)
		}
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	commonEnv := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	platform := runtime.GOOS + "_" + runtime.GOARCH

	createFormula(t, prefix, "pkgA", fmt.Sprintf(`name: pkgA
version: 1.0.0
bottle:
  %s:
    url: %s/pkgA.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, computeSHA256(tarballA)))

	createFormula(t, prefix, "pkgB", fmt.Sprintf(`name: pkgB
version: 1.0.0
bottle:
  %s:
    url: %s/pkgB.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, computeSHA256(tarballB)))

	// 1. Install pkgA
	cmdA := exec.Command(exePath, "install", "pkgA")
	cmdA.Env = commonEnv
	if err := cmdA.Run(); err != nil {
		t.Fatalf("failed to install pkgA: %v", err)
	}

	// 2. Install pkgB - should conflict on bin/dummybin
	cmdB := exec.Command(exePath, "install", "pkgB")
	cmdB.Env = commonEnv
	out, err := cmdB.CombinedOutput()
	if err == nil {
		t.Error("expected conflict error when installing pkgB, but got none")
	}
	if !strings.Contains(string(out), "conflict") && !strings.Contains(string(out), "already exists") {
		t.Errorf("expected conflict error message, got: %s", string(out))
	}

	// 3. Unlink pkgA
	cmdUnlink := exec.Command(exePath, "unlink", "pkgA")
	cmdUnlink.Env = commonEnv
	if err := cmdUnlink.Run(); err != nil {
		t.Fatalf("failed to unlink pkgA: %v", err)
	}

	// 4. Now install pkgB should succeed
	cmdB2 := exec.Command(exePath, "install", "pkgB")
	cmdB2.Env = commonEnv
	if err := cmdB2.Run(); err != nil {
		t.Fatalf("failed to install pkgB after unlinking pkgA: %v", err)
	}
}
