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

func TestOfflineCacheIntegrity(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	tarball := makeDummyTarGz(t, "echo cached")
	tarballHash := computeSHA256(tarball)
	
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	}))
	// We'll close the server later to simulate offline mode
	
	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	platform := runtime.GOOS + "_" + runtime.GOARCH
	createFormula(t, prefix, "cachedpkg", fmt.Sprintf(`name: cachedpkg
version: 1.0.0
bottle:
  %s:
    url: %s/cachedpkg.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, tarballHash))

	commonEnv := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	// 1. First install (downloads and caches)
	cmd1 := exec.Command(exePath, "install", "cachedpkg")
	cmd1.Env = commonEnv
	if err := cmd1.Run(); err != nil {
		t.Fatalf("failed first install: %v", err)
	}

	// 2. Uninstall
	cmdUn := exec.Command(exePath, "uninstall", "cachedpkg")
	cmdUn.Env = commonEnv
	cmdUn.Run()

	// 3. Close server (simulating offline)
	server.Close()

	// 4. Install again - should use cache
	cmd2 := exec.Command(exePath, "install", "cachedpkg")
	cmd2.Env = commonEnv
	if out, err := cmd2.CombinedOutput(); err != nil {
		t.Fatalf("failed second install (expected to use cache): %v\nOutput:\n%s", err, string(out))
	}

	// 5. Verify it's installed
	if _, err := os.Stat(filepath.Join(prefix, "bin", "dummybin")); err != nil {
		t.Error("binary missing after cached install")
	}
}
