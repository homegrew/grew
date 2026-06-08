package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/homegrew/grew/pkg/testhelper"
)

// makeGrewTarGz creates a simple .tar.gz containing a single executable file "grew"
func makeGrewTarGz(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:     "grew",
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

// setupMockGitHub creates an httptest.TLSServer mocking the GitHub API.
func setupMockGitHub(t *testing.T, version string) *httptest.Server {
	t.Helper()

	// The dummy binary just prints its version and exits
	mockGrewContent := fmt.Sprintf("#!/bin/sh\necho \"grew v%s\"\n", version)
	tarballBytes := makeGrewTarGz(t, mockGrewContent)
	tarballHash256 := testhelper.ComputeSHA256(tarballBytes)
	tarballHash512 := testhelper.ComputeSHA512(tarballBytes)

	osName := runtime.GOOS
	archName := runtime.GOARCH
	switch osName {
	case "darwin":
		osName = "Darwin"
	}
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	assetName := fmt.Sprintf("grew_%s_%s.tar.gz", osName, archName)

	checksumsTxt := fmt.Sprintf("%s  %s\n%s  %s\n", tarballHash256, assetName, tarballHash512, assetName)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/homegrew/grew/releases":
			// Mock releases JSON
			w.Header().Set("Content-Type", "application/vnd.github+json")
			// Create a mock URL using the current request's host scheme
			scheme := "https"
			if r.TLS == nil {
				scheme = "http"
			}
			baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

			resp := []map[string]interface{}{
				{
					"tag_name":   "v" + version,
					"draft":      false,
					"prerelease": false,
					"assets": []map[string]interface{}{
						{
							"name":                 assetName,
							"browser_download_url": baseURL + "/download/" + assetName,
						},
						{
							"name":                 "checksums.txt",
							"browser_download_url": baseURL + "/download/checksums.txt",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/download/" + assetName:
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(tarballBytes)
		case "/download/checksums.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(checksumsTxt))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return server
}

// TestRunSelfUpdateIntegration tests the RunSelfUpdate function end-to-end.
// It compiles a dummy binary that invokes cmd.RunSelfUpdate(), places it in a
// mock grew prefix, and executes it. This simulates a real self-update where
// the binary replaces itself. Since there is no git repository in the mock prefix,
// it falls back to downloading the release from GitHub.
func TestRunSelfUpdateIntegration(t *testing.T) {
	tmpDir := t.TempDir()

	mockServer := setupMockGitHub(t, "9.9.9")
	defer mockServer.Close()

	// Export the server's certificate so the testbin can trust it
	certFile := filepath.Join(tmpDir, "server.crt")
	if err := testhelper.WriteServerCert(mockServer, certFile); err != nil {
		t.Fatalf("failed to write server certificate: %v", err)
	}

	// Create prefix structure
	prefix := filepath.Join(tmpDir, "prefix")
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	tmpHomegrewDir := filepath.Join(prefix, "tmp")
	if err := os.MkdirAll(tmpHomegrewDir, 0755); err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	exePath := filepath.Join(binDir, "grew")

	// Compile the dummy binary
	root := testhelper.GetProjectRoot(t)
	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", exePath, filepath.Join(root, "tests", "testbin", "main.go"))
	cmdBuild.Stdout = os.Stdout
	cmdBuild.Stderr = os.Stderr
	if err := cmdBuild.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}

	// Run the dummy binary
	cmdRun := exec.Command(exePath, "run")
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_GITHUB_API_BASE="+mockServer.URL)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS=127.0.0.1")
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	cmdRun.Env = env

	// The dummy binary will run, detect its path, and download the real grew binary
	// from GitHub to replace itself.
	if err := cmdRun.Run(); err != nil {
		t.Fatalf("RunSelfUpdate failed: %v", err)
	}

	// After running, exePath should now be the real grew binary!
	cmdVerify := exec.Command(exePath, "--version")
	out, err := cmdVerify.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run replaced binary: %v, output: %s", err, string(out))
	}
	if len(out) == 0 {
		t.Errorf("expected version output, got empty")
	}
}

// TestSelfUpdateFromReleaseIntegration tests the SelfUpdateFromRelease function
// end-to-end by explicitly calling it.
func TestSelfUpdateFromReleaseIntegration(t *testing.T) {
	tmpDir := t.TempDir()

	mockServer := setupMockGitHub(t, "9.9.9")
	defer mockServer.Close()

	// Export the server's certificate so the testbin can trust it
	certFile := filepath.Join(tmpDir, "server.crt")
	if err := testhelper.WriteServerCert(mockServer, certFile); err != nil {
		t.Fatalf("failed to write server certificate: %v", err)
	}

	// Create prefix structure
	prefix := filepath.Join(tmpDir, "prefix")
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed to create bin dir: %v", err)
	}
	tmpHomegrewDir := filepath.Join(prefix, "tmp")
	if err := os.MkdirAll(tmpHomegrewDir, 0755); err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}
	exePath := filepath.Join(binDir, "grew")

	// Compile the dummy binary
	root := testhelper.GetProjectRoot(t)
	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", exePath, filepath.Join(root, "tests", "testbin", "main.go"))
	cmdBuild.Stdout = os.Stdout
	cmdBuild.Stderr = os.Stderr
	if err := cmdBuild.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}

	// Run the dummy binary
	cmdRun := exec.Command(exePath, "from-release")
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_GITHUB_API_BASE="+mockServer.URL)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS=127.0.0.1")
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	cmdRun.Env = env

	if err := cmdRun.Run(); err != nil {
		t.Fatalf("SelfUpdateFromRelease failed: %v", err)
	}

	// Verify the binary was replaced and works
	cmdVerify := exec.Command(exePath, "--version")
	out, err := cmdVerify.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run replaced binary: %v, output: %s", err, string(out))
	}
	if len(out) == 0 {
		t.Errorf("expected version output, got empty")
	}
}
