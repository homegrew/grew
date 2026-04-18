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

func TestPatchUpdateIntegration(t *testing.T) {
	if _, err := exec.LookPath("bsdiff"); err != nil {
		t.Skip("bsdiff not found, skipping patch update test")
	}
	if _, err := exec.LookPath("bspatch"); err != nil {
		t.Skip("bspatch not found, skipping patch update test")
	}

	tmpDir := t.TempDir()

	// 1. Build the "old" binary (current code)
	prefix := filepath.Join(tmpDir, "prefix")
	binDir := filepath.Join(prefix, "bin")
	os.MkdirAll(binDir, 0755)
	oldExePath := filepath.Join(binDir, "grew")

	cmdBuild := exec.Command("go", "build", "-tags=devmode", "-o", oldExePath, "./testbin/main.go")
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build old binary: %v, output: %s", err, string(out))
	}

	// Determine the current version of the built binary
	out, err := exec.Command(oldExePath, "--version").Output()
	if err != nil {
		t.Fatalf("failed to run --version on built binary: %v", err)
	}
	currentVer := strings.TrimSpace(strings.TrimPrefix(string(out), "grew "))
	if currentVer == "" {
		t.Fatalf("built binary reported empty version (output: %q)", string(out))
	}
	targetVer := "v9.9.9"

	// 2. Create the "new" binary
	newExeContent := fmt.Sprintf("#!/bin/sh\necho \"grew %s\"\n", targetVer)
	newExePath := filepath.Join(tmpDir, "grew-new")
	if err := os.WriteFile(newExePath, []byte(newExeContent), 0755); err != nil {
		t.Fatal(err)
	}

	// 3. Generate the patch
	osName := runtime.GOOS
	archName := runtime.GOARCH
	switch osName {
	case "darwin":
		osName = "Darwin"
	case "linux":
		osName = "Linux"
	}
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	patchName := fmt.Sprintf("grew_%s_%s_%s_to_%s.patch", osName, archName, currentVer, targetVer)
	patchPath := filepath.Join(tmpDir, patchName)
	if out, err := exec.Command("bsdiff", oldExePath, newExePath, patchPath).CombinedOutput(); err != nil {
		t.Fatalf("failed to generate patch: %v, output: %s", err, string(out))
	}

	patchBytes, _ := os.ReadFile(patchPath)
	patchHash256 := computeSHA256(patchBytes)
	patchHash512 := computeSHA512(patchBytes)

	newExeBytes, _ := os.ReadFile(newExePath)
	newExeHash256 := computeSHA256(newExeBytes)
	newExeHash512 := computeSHA512(newExeBytes)
	rawBinName := fmt.Sprintf("grew_%s_%s", osName, archName)

	tarballName := fmt.Sprintf("grew_%s_%s.tar.gz", osName, archName)

	// 4. Setup mock GitHub and OSV
	mockServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme := "https"
		baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

		switch r.URL.Path {
		case "/repos/homegrew/grew/releases":
			resp := []map[string]interface{}{
				{
					"tag_name":   targetVer,
					"draft":      false,
					"prerelease": false,
					"assets": []map[string]interface{}{
						{
							"name":                 tarballName,
							"browser_download_url": baseURL + "/download/" + tarballName,
						},
						{
							"name":                 "checksums.txt",
							"browser_download_url": baseURL + "/download/checksums.txt",
						},
						{
							"name":                 patchName,
							"browser_download_url": baseURL + "/download/" + patchName,
						},
						{
							"name":                 "binary-checksums.txt",
							"browser_download_url": baseURL + "/download/binary-checksums.txt",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/download/" + patchName:
			w.Write(patchBytes)
		case "/download/checksums.txt":
			w.Write([]byte(fmt.Sprintf("%s  %s\n%s  %s\n", patchHash256, patchName, patchHash512, patchName)))
		case "/download/binary-checksums.txt":
			w.Write([]byte(fmt.Sprintf("%s  %s\n%s  %s\n", newExeHash256, rawBinName, newExeHash512, rawBinName)))
		case "/v1/query":
			// Mock OSV response: no vulnerabilities
			w.Write([]byte(`{"vulns": []}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	if err := writeServerCert(mockServer, certFile); err != nil {
		t.Fatalf("failed to write server cert: %v", err)
	}

	// 5. Run the update
	cmdRun := exec.Command(oldExePath, "run")
	env := os.Environ()
	env = append(env, "HOMEGREW_PREFIX="+prefix)
	env = append(env, "HOMEGREW_GITHUB_API_BASE="+mockServer.URL)
	env = append(env, "HOMEGREW_OSV_API_BASE="+mockServer.URL)
	env = append(env, "HOMEGREW_TEST_CERT_FILE="+certFile)
	cmdRun.Env = env
	cmdRun.Stdout = os.Stdout
	cmdRun.Stderr = os.Stderr

	if err := cmdRun.Run(); err != nil {
		t.Fatalf("SelfUpdate with patch failed: %v", err)
	}

	// 6. Verify replaced binary
	out, err := exec.Command(oldExePath, "--version").Output()
	if err != nil {
		t.Fatalf("failed to run replaced binary: %v, output: %s", err, string(out))
	}
	if !strings.Contains(string(out), targetVer) {
		t.Errorf("expected version %s, got %s", targetVer, string(out))
	}
}
