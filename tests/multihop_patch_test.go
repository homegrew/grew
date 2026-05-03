//go:build integration

package tests

import (
	"encoding/json"
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

func TestMultiHopPatchUpdate(t *testing.T) {
	if _, err := exec.LookPath("bsdiff"); err != nil {
		t.Skip("bsdiff not found, skipping patch update test")
	}
	if _, err := exec.LookPath("bspatch"); err != nil {
		t.Skip("bspatch not found, skipping patch update test")
	}

	tmpDir := t.TempDir()

	osName, archName := normalizePlatformNames()
	rawBinName := fmt.Sprintf("grew_%s_%s", osName, archName)

	// Build real binaries for different versions to generate valid patches
	v1Bin := buildBinary(t, tmpDir, "v0.1.0")
	v2Bin := buildBinary(t, tmpDir, "v0.1.1")
	v3Bin := buildBinary(t, tmpDir, "v0.1.2")

	// Generate patches
	p1Name := fmt.Sprintf("grew_%s_%s_v0.1.0_to_v0.1.1.patch", osName, archName)
	p1Path := filepath.Join(tmpDir, p1Name)
	if out, err := exec.Command("bsdiff", v1Bin, v2Bin, p1Path).CombinedOutput(); err != nil {
		t.Fatalf("bsdiff p1 failed: %v\n%s", err, string(out))
	}

	p2Name := fmt.Sprintf("grew_%s_%s_v0.1.1_to_v0.1.2.patch", osName, archName)
	p2Path := filepath.Join(tmpDir, p2Name)
	if out, err := exec.Command("bsdiff", v2Bin, v3Bin, p2Path).CombinedOutput(); err != nil {
		t.Fatalf("bsdiff p2 failed: %v\n%s", err, string(out))
	}

	// Final binary checksums
	v3Data, _ := os.ReadFile(v3Bin)
	v3SHA256 := computeSHA256(v3Data)
	v3SHA512 := computeSHA512(v3Data)
	binaryChecksumsTxt := fmt.Sprintf("%s  %s\n%s  %s\n", v3SHA256, rawBinName, v3SHA512, rawBinName)

	// Patch checksums
	p1Data, _ := os.ReadFile(p1Path)
	p1SHA256 := computeSHA256(p1Data)
	p2Data, _ := os.ReadFile(p2Path)
	p2SHA256 := computeSHA256(p2Data)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseURL := fmt.Sprintf("https://%s", r.Host)
		switch r.URL.Path {
		case "/repos/homegrew/grew/releases":
			resp := []map[string]interface{}{
				{
					"tag_name": "v0.1.2",
					"assets": []map[string]interface{}{
						{"name": p2Name, "browser_download_url": baseURL + "/download/" + p2Name},
						{"name": p2Name + ".sha256", "browser_download_url": baseURL + "/download/" + p2Name + ".sha256"},
						{"name": "binary-checksums.txt", "browser_download_url": baseURL + "/download/binary-checksums.txt"},
						{"name": "checksums.txt", "browser_download_url": baseURL + "/download/checksums.txt"},
					},
				},
				{
					"tag_name": "v0.1.1",
					"assets": []map[string]interface{}{
						{"name": p1Name, "browser_download_url": baseURL + "/download/" + p1Name},
						{"name": p1Name + ".sha256", "browser_download_url": baseURL + "/download/" + p1Name + ".sha256"},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/download/" + p1Name:
			http.ServeFile(w, r, p1Path)
		case "/download/" + p1Name + ".sha256":
			w.Write([]byte(p1SHA256 + "  " + p1Name))
		case "/download/" + p2Name:
			http.ServeFile(w, r, p2Path)
		case "/download/" + p2Name + ".sha256":
			w.Write([]byte(p2SHA256 + "  " + p2Name))
		case "/download/binary-checksums.txt":
			w.Write([]byte(binaryChecksumsTxt))
		case "/download/checksums.txt":
			w.Write([]byte(""))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)

	prefix := filepath.Join(tmpDir, "prefix")
	binDir := filepath.Join(prefix, "bin")
	os.MkdirAll(binDir, 0755)
	exePath := filepath.Join(binDir, "grew")

	// Use v1Bin as our starting point
	v1Data, _ := os.ReadFile(v1Bin)
	os.WriteFile(exePath, v1Data, 0755)

	// Run selfupdate
	cmdRun := exec.Command(exePath, "run")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"))
	env = append(env, "HOMEGREW_GITHUB_API_BASE="+server.URL)
	env = append(env, "HOMEGREW_ALLOWED_HOSTS=127.0.0.1")
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	env = append(env, "GOCACHE="+os.Getenv("GOCACHE")) // Keep GOCACHE if set
	cmdRun.Env = env
	if out, err := cmdRun.CombinedOutput(); err != nil {
		t.Fatalf("selfupdate failed: %v\n%s", err, string(out))
	}

	// Verify the final binary is v0.1.2
	cmdVerify := exec.Command(exePath, "--version")
	out, _ := cmdVerify.CombinedOutput()
	if !strings.Contains(string(out), "0.1.2") {
		t.Errorf("expected version 0.1.2, got: %s", string(out))
	}
}

func buildBinary(t *testing.T, tmpDir, version string) string {
	t.Helper()
	exePath := filepath.Join(tmpDir, "grew-"+version)
	root := getProjectRoot(t)
	// We must use absolute path for the main.go
	mainGo := filepath.Join(root, "tests", "testbin", "main.go")
	cmdBuild := exec.Command("go", "build", "-tags=devmode",
		"-ldflags=-X main.version="+version,
		"-o", exePath, mainGo)
	// Use project's .go-cache to avoid permission issues
	cmdBuild.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".go-cache"))
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build test binary %s: %v\n%s", version, err, string(out))
	}

	t.Logf("built test binary %s: %q", version, exePath)

	return exePath
}

func normalizePlatformNames() (osName, archName string) {
	osName = runtime.GOOS
	archName = runtime.GOARCH
	switch osName {
	case "darwin":
		osName = "Darwin"
	}
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	return
}
