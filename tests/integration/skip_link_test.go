
package integration

import (
	"github.com/homegrew/grew/tests/testhelper"
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

func TestInstallSkipLinkIntegration(t *testing.T) {
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("failed to eval symlinks for temp dir: %v", err)
	}

	tarballContent := `#!/bin/sh
echo "I am dummybin 1.0"
`
	tarballBytes := makeDummyTarGz(t, tarballContent)
	tarballHash := testhelper.ComputeSHA256(tarballBytes)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(tarballBytes)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	if err := testhelper.WriteServerCert(server, certFile); err != nil {
		t.Fatalf("failed to write server certificate: %v", err)
	}

	serverHost := server.URL[strings.Index(server.URL, "://")+3:]
	if colonIdx := strings.Index(serverHost, ":"); colonIdx != -1 {
		serverHost = serverHost[:colonIdx]
	}

	prefix := filepath.Join(tmpDir, "prefix")
	coreTapDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps")
	if err := os.MkdirAll(filepath.Join(coreTapDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create fake .git dir: %v", err)
	}

	coreFormulaDir := filepath.Join(coreTapDir, "core")
	if err := os.MkdirAll(coreFormulaDir, 0755); err != nil {
		t.Fatalf("failed to create core tap dir: %v", err)
	}

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

	formulaPath := filepath.Join(coreFormulaDir, "dummy.yaml")
	if err := os.WriteFile(formulaPath, []byte(formulaYaml), 0644); err != nil {
		t.Fatalf("failed to write formula yaml: %v", err)
	}

	exePath := filepath.Join(tmpDir, "grew-test")
	root := testhelper.GetProjectRoot(t)
	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", exePath, filepath.Join(root, "tests", "testbin", "main.go"))
	if err := cmdBuild.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}

	// Execute with --skip-link
	cmdRun := exec.Command(exePath, "install", "--skip-link", "dummy")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	cmdRun.Env = env

	if out, err := cmdRun.CombinedOutput(); err != nil {
		t.Fatalf("RunInstall failed: %v\nOutput: %s", err, string(out))
	}

	// Verify the installation
	cellarBin := filepath.Join(prefix, "Cellar", "dummy", "1.0.0", "bin", "dummybin")
	if _, err := os.Stat(cellarBin); os.IsNotExist(err) {
		t.Errorf("expected dummybin in cellar at %s, but not found", cellarBin)
	}

	optLink := filepath.Join(prefix, "opt", "dummy")
	if _, err := os.Readlink(optLink); err == nil {
		t.Errorf("expected NO symlink at %s, but found one", optLink)
	}

	binLink := filepath.Join(prefix, "bin", "dummybin")
	if _, err := os.Readlink(binLink); err == nil {
		t.Errorf("expected NO symlink at %s, but found one", binLink)
	}
}
