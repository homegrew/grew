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
	"sync"
	"testing"
)

func TestConcurrentInstall(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	tarball := makeDummyTarGz(t, "echo concurrent")
	tarballHash := computeSHA256(tarball)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	platform := runtime.GOOS + "_" + runtime.GOARCH
	createFormula(t, prefix, "sharedpkg", fmt.Sprintf(`name: sharedpkg
version: 1.0.0
bottle:
  %s:
    url: %s/sharedpkg.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, tarballHash))

	commonEnv := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"),
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	// Run 3 installs in parallel
	var wg sync.WaitGroup
	results := make(chan error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(exePath, "install", "sharedpkg")
			cmd.Env = commonEnv
			results <- cmd.Run()
		}()
	}

	wg.Wait()
	close(results)

	var successCount int
	for err := range results {
		if err == nil {
			successCount++
		}
	}

	// At least one must succeed. Others might fail with "already installed" or "locked"
	if successCount == 0 {
		t.Error("No concurrent installs succeeded")
	}
	t.Logf("%d/3 concurrent installs returned success", successCount)
}
