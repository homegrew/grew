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

func TestTapPriority(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	tarballCore := makeDummyTarGz(t, "echo core")
	tarballUser := makeDummyTarGz(t, "echo user")
	
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "core") {
			w.Write(tarballCore)
		} else {
			w.Write(tarballUser)
		}
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	platform := runtime.GOOS + "_" + runtime.GOARCH

	// 1. Create formula 'double' in core tap
	createFormula(t, prefix, "double", fmt.Sprintf(`name: double
version: 1.0.0
bottle:
  %s:
    url: %s/core/double.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, computeSHA256(tarballCore)))

	// 2. Create another tap 'user/repo' and formula 'double' there
	userTapDir := filepath.Join(prefix, "Taps", "user", "repo")
	os.MkdirAll(userTapDir, 0755)
	userFormulaPath := filepath.Join(userTapDir, "double.yaml")
	userFormulaYaml := fmt.Sprintf(`name: double
version: 2.0.0
bottle:
  %s:
    url: %s/user/double.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, computeSHA256(tarballUser))
	os.WriteFile(userFormulaPath, []byte(userFormulaYaml), 0644)

	commonEnv := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	// 3. Install 'double' - should pick core tap by default (alphabetical or fixed priority)
	// Actually Grew currently just iterates taps. 
	// Let's see what happens.
	cmd1 := exec.Command(exePath, "install", "double")
	cmd1.Env = commonEnv
	if out, err := cmd1.CombinedOutput(); err != nil {
		t.Fatalf("failed to install double: %v\nOutput:\n%s", err, string(out))
	}

	// Verify it's version 1.0.0 (core)
	out1, _ := exec.Command(exePath, "list", "--versions").CombinedOutput()
	// Wait, 'list' might need the prefix too
	cmdList := exec.Command(exePath, "list", "--versions")
	cmdList.Env = commonEnv
	out1, _ = cmdList.CombinedOutput()
	
	if !strings.Contains(string(out1), "1.0.0") {
		t.Errorf("Expected version 1.0.0 from core tap, but got: %s", string(out1))
	}

	// 4. Uninstall and install specifically from user/repo/double
	cmdUn := exec.Command(exePath, "uninstall", "double")
	cmdUn.Env = commonEnv
	cmdUn.Run() // cleanup
	
	cmd3 := exec.Command(exePath, "install", "user/repo/double")
	cmd3.Env = commonEnv
	if out, err := cmd3.CombinedOutput(); err != nil {
		t.Fatalf("failed to install user/repo/double: %v\nOutput:\n%s", err, string(out))
	}

	cmdList2 := exec.Command(exePath, "list", "--versions")
	cmdList2.Env = commonEnv
	out2, _ := cmdList2.CombinedOutput()
	if !strings.Contains(string(out2), "2.0.0") {
		t.Errorf("Expected version 2.0.0 from user tap, but got: %s", string(out2))
	}
}
