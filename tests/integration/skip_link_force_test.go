package integration

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

	"github.com/homegrew/grew/pkg/testhelper"
)

func TestInstallSkipLinkForceIntegration(t *testing.T) {
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

	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)

	// 1. First install normally (linked)
	cmdRun1 := exec.Command(exePath, "install", "dummy")
	cmdRun1.Env = env
	if out, err := cmdRun1.CombinedOutput(); err != nil {
		t.Fatalf("First install failed: %v\nOutput: %s", err, string(out))
	}

	// Verify linked
	binLink := filepath.Join(prefix, "bin", "dummybin")
	if _, err := os.Readlink(binLink); err != nil {
		t.Errorf("expected symlink at %s, but not found", binLink)
	}

	// 2. Install with --skip-link and --force
	cmdRun2 := exec.Command(exePath, "install", "--skip-link", "--force", "dummy")
	cmdRun2.Env = env
	if out, err := cmdRun2.CombinedOutput(); err != nil {
		t.Fatalf("Second install failed: %v\nOutput: %s", err, string(out))
	}

	// Verify NOT linked
	if _, err := os.Readlink(binLink); err == nil {
		t.Errorf("expected NO symlink at %s (after --skip-link --force), but found one", binLink)
	}
}
