package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homegrew/grew/pkg/testhelper"
)

func TestInterruptedInstallRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := testhelper.SetupPrefix(t, tmpDir)
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Create a large-ish tarball to give us time to interrupt
	content := strings.Repeat("some data\n", 100000)
	tarball := makeDummyTarGz(t, content)
	tarballHash := testhelper.ComputeSHA256(tarball)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow down the transfer
		flusher, ok := w.(http.Flusher)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(tarball)))
		w.WriteHeader(http.StatusOK)
		chunkSize := 1024
		for i := 0; i < len(tarball); i += chunkSize {
			end := i + chunkSize
			if end > len(tarball) {
				end = len(tarball)
			}
			w.Write(tarball[i:end])
			if ok {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	testhelper.WriteServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	platform := runtime.GOOS + "_" + runtime.GOARCH
	testhelper.CreateFormula(t, prefix, "interrupted", fmt.Sprintf(`name: interrupted
version: 1.0.0
bottle:
  %s:
    url: %s/interrupted.tar.gz
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

	// 1. Start installation and kill it after 100ms
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, exePath, "install", "interrupted")
	cmd.Env = commonEnv
	_ = cmd.Run() // Expect failure/timeout

	// 2. Check that the prefix isn't corrupted and we can try again
	// (grew should handle lock cleanup or stale temp files)

	// We need a fresh server or one that isn't throttled for the second attempt
	server2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
	}))
	defer server2.Close()

	testhelper.WriteServerCert(server2, certFile)
	serverHost2 := strings.TrimPrefix(server2.URL, "https://")
	if idx := strings.Index(serverHost2, ":"); idx != -1 {
		serverHost2 = serverHost2[:idx]
	}

	// Update formula with new server URL
	testhelper.CreateFormula(t, prefix, "interrupted", fmt.Sprintf(`name: interrupted
version: 1.0.0
bottle:
  %s:
    url: %s/interrupted.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server2.URL, tarballHash))

	commonEnv2 := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"),
		"HOMEGREW_ALLOWED_HOSTS="+serverHost2,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	cmd2 := exec.Command(exePath, "install", "interrupted")
	cmd2.Env = commonEnv2
	out, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("Recovery install failed: %v, output: %s", err, string(out))
	}

	if _, err := os.Stat(filepath.Join(prefix, "bin", "dummybin")); err != nil {
		t.Error("Binary missing after recovery install")
	}
}
