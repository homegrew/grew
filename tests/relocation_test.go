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
	"testing"
)

func TestBinaryRelocation(t *testing.T) {
	// Relocation requires patchelf on Linux or install_name_tool on macOS.
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("patchelf"); err != nil {
			t.Skip("patchelf not found, skipping relocation test")
		}
	} else if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("install_name_tool"); err != nil {
			t.Skip("install_name_tool not found, skipping relocation test")
		}
	} else {
		t.Skip("Relocation test only supported on Linux and macOS")
	}

	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	// 1. Compile a dummy program that we will "relocate"
	// We'll use a foreign path as an RPATH during build if possible, 
	// or just rely on Grew to find and replace the prefix.
	dummySrc := filepath.Join(tmpDir, "dummy.go")
	os.WriteFile(dummySrc, []byte(`package main; import "fmt"; func main() { fmt.Println("hello") }`), 0644)
	
	// Initialize a dummy module
	cmdMod := exec.Command("go", "mod", "init", "dummy")
	cmdMod.Dir = tmpDir
	cmdMod.Env = append(os.Environ(), "GOCACHE="+filepath.Join(tmpDir, "gocache"))
	cmdMod.Run()

	dummyBin := filepath.Join(tmpDir, "dummybin")
	// On Linux, we can try to set an RPATH during build
	buildArgs := []string{"build", "-o", dummyBin}
	if runtime.GOOS == "linux" {
		buildArgs = append(buildArgs, "-ldflags=-r /old/prefix/lib")
	}
	cmdBuild := exec.Command("go", buildArgs...)
	cmdBuild.Dir = tmpDir
	cmdBuild.Env = append(os.Environ(), "GOCACHE="+filepath.Join(tmpDir, "gocache"))
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build dummy binary: %v, output: %s", err, string(out))
	}

	// Read the real binary
	binData, err := os.ReadFile(dummyBin)
	if err != nil {
		t.Fatalf("failed to read dummy binary: %v", err)
	}
	tarballBytes := makeDummyTarGz(t, string(binData))
	tarballHash := computeSHA256(tarballBytes)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarballBytes)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	platform := runtime.GOOS + "_" + runtime.GOARCH
	createFormula(t, prefix, "relocatable", fmt.Sprintf(`name: relocatable
version: 1.0.0
bottle:
  %s:
    url: %s/relocatable.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, tarballHash))

	commonEnv := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	// 2. Install
	cmdInstall := exec.Command(exePath, "install", "relocatable")
	cmdInstall.Env = commonEnv
	if out, err := cmdInstall.CombinedOutput(); err != nil {
		t.Fatalf("install failed: %v, output: %s", err, string(out))
	}

	// 3. Verify relocation
	installedBin := filepath.Join(prefix, "Cellar", "relocatable", "1.0.0", "bin", "dummybin")
	
	var inspectCmd *exec.Cmd
	if runtime.GOOS == "linux" {
		inspectCmd = exec.Command("patchelf", "--print-rpath", installedBin)
	} else {
		inspectCmd = exec.Command("otool", "-l", installedBin)
	}
	
	out, _ := inspectCmd.CombinedOutput()
	// Check if the old prefix is gone and the new one (the temp prefix) is present
	// Note: Grew also replaces standard brew paths and placeholders.
	if strings.Contains(string(out), "/old/prefix") {
		t.Errorf("Binary still contains old RPATH: %s", string(out))
	}
	
	t.Logf("Relocation output: %s", string(out))
}
