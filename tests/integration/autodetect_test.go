package integration

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
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

// makeTarGz creates a simple .tar.gz containing a single executable file "bin/<binname>"
func makeTarGz(t *testing.T, binname, content string) []byte {
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
		Name:     "bin/" + binname,
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

// makeAppZip creates a zip file containing a dummy .app bundle
func makeAppZip(t *testing.T, appName string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	files := map[string]string{
		appName + "/Contents/Info.plist": `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>App</string>
</dict>
</plist>`,
		appName + "/Contents/MacOS/App": "#!/bin/sh\necho \"App\"\n",
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

// createTestFormula creates a formula YAML in the core tap directory
func createTestFormula(t *testing.T, prefix, name string, serverURL string, tarballHash string) {
	t.Helper()
	coreTapDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps", "core")
	if err := os.MkdirAll(coreTapDir, 0755); err != nil {
		t.Fatalf("failed to create core tap dir: %v", err)
	}

	platformKey := runtime.GOOS + "_" + runtime.GOARCH
	formulaYaml := fmt.Sprintf(`name: %s
version: 1.0.0
description: A test formula
homepage: https://example.com
license: MIT
bottle:
  %s:
    url: %s/%s-1.0.0.tar.gz
    sha256: %s
install:
  type: archive
  format: tar.gz
  strip_components: 0
`, name, platformKey, serverURL, name, tarballHash)

	formulaPath := filepath.Join(coreTapDir, name+".yaml")
	if err := os.WriteFile(formulaPath, []byte(formulaYaml), 0644); err != nil {
		t.Fatalf("failed to write formula yaml: %v", err)
	}
}

// createTestCask creates a cask YAML in the cask tap directory
func createTestCask(t *testing.T, prefix, name string, serverURL string, zipHash string) {
	t.Helper()
	caskDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps", "cask")
	if err := os.MkdirAll(caskDir, 0755); err != nil {
		t.Fatalf("failed to create cask tap dir: %v", err)
	}

	platformKey := runtime.GOOS + "_" + runtime.GOARCH
	appName := strings.Title(strings.TrimSuffix(name, "cask")) + ".app"
	caskYaml := fmt.Sprintf(`name: %s
version: 1.0.0
description: A test cask
homepage: https://example.com
url:
  %s: %s/%s.zip
sha256:
  %s: %s
artifacts:
  app:
    - %s
`, name, platformKey, serverURL, name, platformKey, zipHash, appName)

	caskPath := filepath.Join(caskDir, name+".yaml")
	if err := os.WriteFile(caskPath, []byte(caskYaml), 0644); err != nil {
		t.Fatalf("failed to write cask yaml: %v", err)
	}
}

// setupTestEnvironment creates prefix and mock server, returns prefix, serverURL, certFile, serverHost, and cleanup function
func setupTestEnvironment(t *testing.T) (string, string, string, string, func()) {
	t.Helper()

	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("failed to eval symlinks for temp dir: %v", err)
	}

	prefix := filepath.Join(tmpDir, "prefix")
	// Initialize git repo in core tap to prevent auto-cloning
	coreTapDir := filepath.Join(prefix, "Taps", "homegrew", "homegrew-taps")
	if err := os.MkdirAll(filepath.Join(coreTapDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create fake .git dir: %v", err)
	}
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = coreTapDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init git in core tap: %v, output: %s", err, string(out))
	}

	// Create Applications directory for casks
	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create Applications dir: %v", err)
	}

	// Mock HTTP server (serves both formulas and casks)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Header().Set("Content-Type", "application/gzip")
			tarballContent := `#!/bin/sh
echo "test binary"
`
			tarballBytes := makeTarGz(t, "testbin", tarballContent)
			w.Write(tarballBytes)
		} else if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Header().Set("Content-Type", "application/zip")
			zipBytes := makeAppZip(t, "TestApp.app")
			w.Write(zipBytes)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	certFile := filepath.Join(tmpDir, "server.crt")
	if err := testhelper.WriteServerCert(server, certFile); err != nil {
		t.Fatalf("failed to write server certificate: %v", err)
	}

	serverHost := server.URL[strings.Index(server.URL, "://")+3:]
	if colonIdx := strings.Index(serverHost, ":"); colonIdx != -1 {
		serverHost = serverHost[:colonIdx]
	}

	cleanup := func() {
		server.Close()
	}

	return prefix, server.URL, certFile, serverHost, cleanup
}

// TestAutoDetect_FormulaNoAmbiguity tests installing a formula without flags when only formula exists
func TestAutoDetect_FormulaNoAmbiguity(t *testing.T) {
	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create formula
	tarballContent := `#!/bin/sh
echo "auto-formula"
`
	tarballBytes := makeTarGz(t, "auto-formula", tarballContent)
	tarballHash := testhelper.ComputeSHA256(tarballBytes)
	createTestFormula(t, prefix, "auto-formula", serverURL, tarballHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Run install without flags
	cmd := exec.Command(exePath, "install", "auto-formula")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		t.Fatalf("install auto-formula failed: %v", err)
	}

	// Verify formula was installed in cellar
	cellarBin := filepath.Join(prefix, "Cellar", "auto-formula", "1.0.0", "bin", "auto-formula")
	if _, err := os.Stat(cellarBin); os.IsNotExist(err) {
		t.Errorf("expected auto-formula in cellar at %s, but not found", cellarBin)
	}
}

// TestAutoDetect_CaskFallback tests installing a cask without flags when only cask exists
func TestAutoDetect_CaskFallback(t *testing.T) {
	if _, err := exec.LookPath("xattr"); err != nil {
		t.Skip("xattr not found, skipping macOS specific test")
	}

	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create Applications directory
	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create Applications dir: %v", err)
	}

	// Create cask
	zipBytes := makeAppZip(t, "Auto.app")
	zipHash := testhelper.ComputeSHA256(zipBytes)
	createTestCask(t, prefix, "auto-cask", serverURL, zipHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Run install without flags
	cmd := exec.Command(exePath, "install", "auto-cask")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_APPDIR="+appDir)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		t.Fatalf("install auto-cask failed: %v", err)
	}

	// Verify cask was installed
	installedApp := filepath.Join(appDir, "Auto.app")
	if _, err := os.Stat(installedApp); err != nil {
		t.Fatalf("app was not installed to %s", installedApp)
	}
}

// TestAutoDetect_FormulaWinsWhenBoth tests that formula takes precedence when both exist
func TestAutoDetect_FormulaWinsWhenBoth(t *testing.T) {
	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create Applications directory
	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create Applications dir: %v", err)
	}

	// Create both formula and cask with same name
	tarballContent := `#!/bin/sh
echo "both-pkg"
`
	tarballBytes := makeTarGz(t, "both-pkg", tarballContent)
	tarballHash := testhelper.ComputeSHA256(tarballBytes)
	createTestFormula(t, prefix, "both-pkg", serverURL, tarballHash)

	zipBytes := makeAppZip(t, "Both.app")
	zipHash := testhelper.ComputeSHA256(zipBytes)
	createTestCask(t, prefix, "both-pkg", serverURL, zipHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Run install without flags (should install formula)
	cmd := exec.Command(exePath, "install", "both-pkg")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_APPDIR="+appDir)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		t.Fatalf("install both-pkg failed: %v", err)
	}

	// Verify formula was installed (not cask)
	cellarBin := filepath.Join(prefix, "Cellar", "both-pkg", "1.0.0", "bin", "both-pkg")
	if _, err := os.Stat(cellarBin); os.IsNotExist(err) {
		t.Errorf("expected formula in cellar at %s, but not found", cellarBin)
	}

	// Verify cask was NOT installed
	installedApp := filepath.Join(appDir, "Both.app")
	if _, err := os.Stat(installedApp); !os.IsNotExist(err) {
		t.Errorf("cask should not have been installed to %s when formula exists", installedApp)
	}
}

// TestAutoDetect_FormulaFlag_Success tests --formula flag with a formula that exists
func TestAutoDetect_FormulaFlag_Success(t *testing.T) {
	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create formula
	tarballContent := `#!/bin/sh
echo "formula-only"
`
	tarballBytes := makeTarGz(t, "formula-only", tarballContent)
	tarballHash := testhelper.ComputeSHA256(tarballBytes)
	createTestFormula(t, prefix, "formula-only", serverURL, tarballHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Run install with --formula flag
	cmd := exec.Command(exePath, "install", "--formula", "formula-only")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		t.Fatalf("install --formula formula-only failed: %v", err)
	}

	// Verify formula was installed
	cellarBin := filepath.Join(prefix, "Cellar", "formula-only", "1.0.0", "bin", "formula-only")
	if _, err := os.Stat(cellarBin); os.IsNotExist(err) {
		t.Errorf("expected formula in cellar at %s, but not found", cellarBin)
	}
}

// TestAutoDetect_FormulaFlag_NoMatch tests --formula flag when only cask exists
func TestAutoDetect_FormulaFlag_NoMatch(t *testing.T) {
	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create Applications directory
	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create Applications dir: %v", err)
	}

	// Create only cask
	zipBytes := makeAppZip(t, "Cask.app")
	zipHash := testhelper.ComputeSHA256(zipBytes)
	createTestCask(t, prefix, "cask-only", serverURL, zipHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Run install with --formula flag (should fail)
	cmd := exec.Command(exePath, "install", "--formula", "cask-only")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_APPDIR="+appDir)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmd.Env = env

	if err := cmd.Run(); err == nil {
		t.Fatalf("install --formula cask-only should have failed, but succeeded")
	}

	// Verify error message contains "not found" or "formula"
	stderrStr := stderr.String()
	if !strings.Contains(strings.ToLower(stderrStr), "not found") && !strings.Contains(strings.ToLower(stderrStr), "formula") {
		t.Errorf("expected error message to contain 'not found' or 'formula', got: %s", stderrStr)
	}
}

// TestAutoDetect_CaskFlag_NoMatch tests --cask flag when only formula exists
func TestAutoDetect_CaskFlag_NoMatch(t *testing.T) {
	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create only formula
	tarballContent := `#!/bin/sh
echo "formula-only"
`
	tarballBytes := makeTarGz(t, "formula-only", tarballContent)
	tarballHash := testhelper.ComputeSHA256(tarballBytes)
	createTestFormula(t, prefix, "formula-only", serverURL, tarballHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Run install with --cask flag (should fail)
	cmd := exec.Command(exePath, "install", "--cask", "formula-only")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmd.Env = env

	if err := cmd.Run(); err == nil {
		t.Fatalf("install --cask formula-only should have failed, but succeeded")
	}

	// Verify error message contains "not found" or "cask"
	stderrStr := stderr.String()
	if !strings.Contains(strings.ToLower(stderrStr), "not found") && !strings.Contains(strings.ToLower(stderrStr), "cask") {
		t.Errorf("expected error message to contain 'not found' or 'cask', got: %s", stderrStr)
	}
}

// TestAutoDetect_MutuallyExclusive tests that --formula and --cask together produce an error
func TestAutoDetect_MutuallyExclusive(t *testing.T) {
	prefix, _, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Run install with both --formula and --cask flags (should fail)
	cmd := exec.Command(exePath, "install", "--formula", "--cask", "somepkg")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmd.Env = env

	if err := cmd.Run(); err == nil {
		t.Fatalf("install --formula --cask should have failed, but succeeded")
	}

	// Verify error message contains "mutually exclusive"
	stderrStr := stderr.String()
	if !strings.Contains(strings.ToLower(stderrStr), "mutually exclusive") && !strings.Contains(strings.ToLower(stderrStr), "cannot be used together") {
		t.Errorf("expected error message to contain 'mutually exclusive' or 'cannot be used together', got: %s", stderrStr)
	}
}

// TestAutoDetect_UninstallCaskFallback tests uninstalling a cask without flags
func TestAutoDetect_UninstallCaskFallback(t *testing.T) {
	if _, err := exec.LookPath("xattr"); err != nil {
		t.Skip("xattr not found, skipping macOS specific test")
	}

	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create Applications directory
	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create Applications dir: %v", err)
	}

	// Create cask
	zipBytes := makeAppZip(t, "Uninstall.app")
	zipHash := testhelper.ComputeSHA256(zipBytes)
	createTestCask(t, prefix, "auto-cask", serverURL, zipHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Install the cask
	cmdInstall := exec.Command(exePath, "install", "auto-cask")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_APPDIR="+appDir)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmdInstall.Env = env

	if err := cmdInstall.Run(); err != nil {
		t.Fatalf("install auto-cask failed: %v", err)
	}

	// Verify cask was installed
	installedApp := filepath.Join(appDir, "Uninstall.app")
	if _, err := os.Stat(installedApp); err != nil {
		t.Fatalf("app was not installed to %s", installedApp)
	}

	// Now uninstall without flags
	cmdUninstall := exec.Command(exePath, "uninstall", "auto-cask")
	cmdUninstall.Env = env

	if err := cmdUninstall.Run(); err != nil {
		t.Fatalf("uninstall auto-cask failed: %v", err)
	}

	// Verify cask was uninstalled
	if _, err := os.Stat(installedApp); !os.IsNotExist(err) {
		t.Fatalf("app was not removed from %s after uninstall", installedApp)
	}
}

// TestAutoDetect_InfoCaskFallback tests running info on a cask without flags
func TestAutoDetect_InfoCaskFallback(t *testing.T) {
	prefix, serverURL, certFile, serverHost, cleanup := setupTestEnvironment(t)
	defer cleanup()

	tmpDir := filepath.Dir(prefix)

	// Create Applications directory
	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("failed to create Applications dir: %v", err)
	}

	// Create cask
	zipBytes := makeAppZip(t, "Info.app")
	zipHash := testhelper.ComputeSHA256(zipBytes)
	createTestCask(t, prefix, "auto-cask", serverURL, zipHash)

	// Build test binary
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	// Install the cask first
	cmdInstall := exec.Command(exePath, "install", "auto-cask")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_APPDIR="+appDir)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS="+serverHost)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "HOMEGREW_NO_INIT_TAP=1")
	cmdInstall.Env = env

	if err := cmdInstall.Run(); err != nil {
		t.Fatalf("install auto-cask failed: %v", err)
	}

	// Now run info without flags
	cmdInfo := exec.Command(exePath, "info", "auto-cask")
	var stdout bytes.Buffer
	cmdInfo.Stdout = &stdout
	cmdInfo.Env = env

	if err := cmdInfo.Run(); err != nil {
		t.Fatalf("info auto-cask failed: %v", err)
	}

	// Verify output contains the cask name
	stdoutStr := stdout.String()
	if !strings.Contains(stdoutStr, "auto-cask") {
		t.Errorf("expected info output to contain 'auto-cask', got: %s", stdoutStr)
	}
}
