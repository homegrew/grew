//go:build integration

package tests

import (
	"archive/zip"
	"bytes"
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

// makeDummyAppZip creates a zip file containing a dummy .app bundle
func makeDummyAppZip(t *testing.T, appName string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	files := map[string]string{
		appName + "/Contents/Info.plist": `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>DummyApp</string>
</dict>
</plist>`,
		appName + "/Contents/MacOS/DummyApp": "#!/bin/sh\necho \"Dummy App\"\n",
	}

	for path, content := range files {
		f, err := w.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write([]byte(content))
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCaskInstallIntegration(t *testing.T) {
	if _, err := exec.LookPath("xattr"); err != nil {
		t.Skip("xattr not found, skipping macOS specific test")
	}

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("failed to eval symlinks for temp dir: %v", err)
	}

	zipBytes := makeDummyAppZip(t, "Dummy.app")
	zipHash := computeSHA256(zipBytes)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipBytes)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	if err := writeServerCert(server, certFile); err != nil {
		t.Fatalf("failed to write server certificate: %v", err)
	}

	serverHost := server.URL[strings.Index(server.URL, "://")+3:]
	if colonIdx := strings.Index(serverHost, ":"); colonIdx != -1 {
		serverHost = serverHost[:colonIdx]
	}

	prefix := filepath.Join(tmpDir, "prefix")
	caskDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps", "cask")
	if err := os.MkdirAll(caskDir, 0755); err != nil {
		t.Fatalf("failed to create cask tap dir: %v", err)
	}
	// fake git
	if err := os.MkdirAll(filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps", ".git"), 0755); err != nil {
		t.Fatalf("failed to create fake .git dir: %v", err)
	}

	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create Applications dir: %v", err)
	}

	platformKey := runtime.GOOS + "_" + runtime.GOARCH
	caskYaml := fmt.Sprintf(`name: dummycask
version: 1.0.0
description: A dummy cask
homepage: https://example.com
url:
  %s: %s/dummy.zip
sha256:
  %s: %s
artifacts:
  app:
    - Dummy.app
`, platformKey, server.URL, platformKey, zipHash)

	caskPath := filepath.Join(caskDir, "dummycask.yaml")
	if err := os.WriteFile(caskPath, []byte(caskYaml), 0644); err != nil {
		t.Fatalf("failed to write cask yaml: %v", err)
	}

	exePath := buildTestBinary(t, tmpDir)

	// Test 1: Install WITH quarantine (default)
	cmdRun := exec.Command(exePath, "install", "--cask", "dummycask")
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr

	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_APPDIR="+appDir)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	cmdRun.Env = env

	if err := cmdRun.Run(); err != nil {
		t.Fatalf("cask install failed: %v", err)
	}

	installedApp := filepath.Join(appDir, "Dummy.app")
	if _, err := os.Stat(installedApp); err != nil {
		t.Fatalf("app was not installed to %s", installedApp)
	}

	// Verify quarantine attribute
	out, err := exec.Command("xattr", "-p", "com.apple.quarantine", "--", installedApp).CombinedOutput()
	if err != nil {
		t.Fatalf("expected quarantine attribute to be set, but xattr failed: %v (output: %s)", err, out)
	}
	// The attribute should at least exist. Modern macOS might not put 'grew' in the string
	// if set via the native API, but it often looks like '0081;...;grew;' or '0281;...;;UUID'
	if len(out) == 0 {
		t.Errorf("quarantine attribute is empty: %s", out)
	}

	// Test 2: Uninstall (should move to Trash or fall back to rm)
	// Just verify it's removed from AppDir
	cmdUninstall := exec.Command(exePath, "uninstall", "--cask", "dummycask")
	cmdUninstall.Stdout = os.Stdout
	cmdUninstall.Stderr = os.Stderr
	cmdUninstall.Env = env
	if err := cmdUninstall.Run(); err != nil {
		t.Fatalf("cask uninstall failed: %v", err)
	}

	if _, err := os.Stat(installedApp); !os.IsNotExist(err) {
		t.Fatalf("app was not removed from %s after uninstall", installedApp)
	}

	// Test 3: Install WITHOUT quarantine
	cmdRunNoQuarantine := exec.Command(exePath, "install", "--cask", "dummycask", "--no-quarantine")
	cmdRunNoQuarantine.Stdout = os.Stdout
	cmdRunNoQuarantine.Stderr = os.Stderr
	cmdRunNoQuarantine.Env = env

	if err := cmdRunNoQuarantine.Run(); err != nil {
		t.Fatalf("cask install --no-quarantine failed: %v", err)
	}

	if _, err := os.Stat(installedApp); err != nil {
		t.Fatalf("app was not installed to %s", installedApp)
	}

	// Verify quarantine attribute is NOT set
	out, err = exec.Command("xattr", "-p", "com.apple.quarantine", "--", installedApp).CombinedOutput()
	if err == nil {
		t.Fatalf("expected quarantine attribute to NOT be set, but xattr succeeded: %s", out)
	}
}
