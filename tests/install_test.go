package tests

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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

// makeDummyTarGz creates a simple .tar.gz containing a single executable file "bin/dummybin"
func makeDummyTarGz(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add bin/ directory
	dirHdr := &tar.Header{
		Name:     "bin/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	if err := tw.WriteHeader(dirHdr); err != nil {
		t.Fatal(err)
	}

	hdr := &tar.Header{
		Name:     "bin/dummybin",
		Size:     int64(len(content)),
		Mode:     0755, // Executable
		Typeflag: tar.TypeReg,
	}

	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gw.Close()
	return buf.Bytes()
}

// computeSHA256 returns the hex-encoded SHA256 of the given bytes.
func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// TestInstallIntegration tests the RunInstall function end-to-end.
// It sets up a mock HTTP server to serve a dummy tarball (bottle),
// creates a local tap directory with a formula pointing to the local server,
// and then executes the install command through our test binary.
func TestInstallIntegration(t *testing.T) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("failed to eval symlinks for temp dir: %v", err)
	}

	// 1. Create a dummy tarball that will be our downloaded "bottle"
	tarballContent := `#!/bin/sh
echo "I am dummybin 1.0"
`
	tarballBytes := makeDummyTarGz(t, tarballContent)
	tarballHash := computeSHA256(tarballBytes)

	// 2. Setup a mock HTTPS server to serve the tarball
	// We must use a host that the Downloader's allowlist will accept.
	// We can add our test server's host to the allowed hosts via an env variable.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(tarballBytes)
	}))
	defer server.Close()

	// Export the server's certificate so the testbin can trust it
	certFile := filepath.Join(tmpDir, "server.crt")
	if err := writeServerCert(server, certFile); err != nil {
		t.Fatalf("failed to write server certificate: %v", err)
	}

	// Extract host without port from server.URL
	serverHost := server.URL[strings.Index(server.URL, "://")+3:]
	if colonIdx := strings.Index(serverHost, ":"); colonIdx != -1 {
		serverHost = serverHost[:colonIdx]
	}

	// 3. Create the Grew prefix structure
	prefix := filepath.Join(tmpDir, "prefix")
	// The prefix init requires these directories to exist, but `Paths.Init()` inside `installContext` handles it.
	
	// 4. Create the core tap and a formula definition
	// To prevent the tap manager from cloning the real homegrew-taps repository
	// over our mock data, we create a fake .git directory inside the Taps dir.
	tapsDir := filepath.Join(prefix, "Taps")
	if err := os.MkdirAll(filepath.Join(tapsDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create fake .git dir: %v", err)
	}

	coreTapDir := filepath.Join(tapsDir, "core")
	if err := os.MkdirAll(coreTapDir, 0755); err != nil {
		t.Fatalf("failed to create core tap dir: %v", err)
	}

	// Formula YAML definition
	// Notice we point the URL to our mock HTTP server.
	platformKey := runtime.GOOS + "_" + runtime.GOARCH
	formulaYaml := fmt.Sprintf(`name: dummy
version: 1.0.0
description: A dummy package
homepage: https://example.com
license: MIT
bottle:
  %s:
    url: %s/dummy-1.0.0.tar.gz
    sha256: %s
install:
  type: archive
  format: tar.gz
  strip_components: 0
`, platformKey, server.URL, tarballHash)

	formulaPath := filepath.Join(coreTapDir, "dummy.yaml")
	if err := os.WriteFile(formulaPath, []byte(formulaYaml), 0644); err != nil {
		t.Fatalf("failed to write formula yaml: %v", err)
	}

	// 5. Build our test executable that will invoke cmd.RunInstall
	exePath := filepath.Join(tmpDir, "grew-test")
	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", exePath, "./testbin/main.go")
	cmdBuild.Stdout = os.Stdout
	cmdBuild.Stderr = os.Stderr
	if err := cmdBuild.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}

	// 6. Execute the install command
	cmdRun := exec.Command(exePath, "install", "dummy")
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr
	
	env := os.Environ()
	// Set the prefix
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	// Add the mock server to the allowed hosts for the downloader
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	// Inject the trusted certificate
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	cmdRun.Env = env

	if err := cmdRun.Run(); err != nil {
		t.Fatalf("RunInstall failed: %v", err)
	}

	// 7. Verify the installation
	// Check if the dummybin was installed in the cellar and symlinked to opt/ and bin/
	cellarBin := filepath.Join(prefix, "Cellar", "dummy", "1.0.0", "bin", "dummybin")
	if _, err := os.Stat(cellarBin); os.IsNotExist(err) {
		t.Errorf("expected dummybin in cellar at %s, but not found", cellarBin)
		filepath.Walk(filepath.Join(prefix, "Cellar", "dummy", "1.0.0"), func(path string, info os.FileInfo, err error) error {
			t.Logf("Found in cellar: %s", path)
			return nil
		})
	}

	optLink := filepath.Join(prefix, "opt", "dummy")
	if _, err := os.Readlink(optLink); err != nil {
		t.Errorf("expected symlink at %s, but not found or not a symlink", optLink)
	}

	binLink := filepath.Join(prefix, "bin", "dummybin")
	if _, err := os.Readlink(binLink); err != nil {
		t.Errorf("expected symlink at %s, but not found or not a symlink", binLink)
	}
}
