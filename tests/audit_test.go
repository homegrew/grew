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

func TestAuditVulnerability(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	tarball := makeDummyTarGz(t, "echo vulnerable")
	tarballHash := computeSHA256(tarball)

	// Mock OSV server
	osvServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/querybatch" {
			// Return a vulnerability for 'vulnerablepkg' version 1.0.0
			resp := map[string]interface{}{
				"results": []map[string]interface{}{
					{
						"vulns": []map[string]interface{}{
							{
								"id":      "CVE-2026-1234",
								"summary": "Nasty bug in vulnerablepkg",
								"database_specific": map[string]interface{}{
									"severity": "HIGH",
								},
							},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else if r.URL.Path == "/vulns/CVE-2026-1234" {
			resp := map[string]interface{}{
				"id":      "CVE-2026-1234",
				"summary": "Nasty bug in vulnerablepkg",
				"details": "Nasty bug in vulnerablepkg",
				"database_specific": map[string]interface{}{
					"severity": "HIGH",
				},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			w.Write(tarball)
		}
	}))
	defer osvServer.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	writeServerCert(osvServer, certFile)
	serverHost := strings.TrimPrefix(osvServer.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	platform := runtime.GOOS + "_" + runtime.GOARCH
	createFormula(t, prefix, "vulnerablepkg", fmt.Sprintf(`name: vulnerablepkg
version: 1.0.0
homepage: https://github.com/user/vulnerablepkg
bottle:
  %s:
    url: %s/vulnerablepkg.tar.gz
    sha256: %s
install:
  type: archive
`, platform, osvServer.URL, tarballHash))

	commonEnv := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
		"HOMEGREW_OSV_API_BASE="+osvServer.URL,
	)

	// 1. Install the vulnerable package
	cmdInstall := exec.Command(exePath, "install", "vulnerablepkg")
	cmdInstall.Env = commonEnv
	if err := cmdInstall.Run(); err != nil {
		t.Fatalf("failed to install vulnerablepkg: %v", err)
	}

	// 2. Run vuln-scan
	cmdScan := exec.Command(exePath, "vuln-scan", "vulnerablepkg")
	cmdScan.Env = commonEnv
	out, err := cmdScan.CombinedOutput()
	
	// Expect it to find vulnerabilities (exit non-zero)
	if err == nil {
		t.Error("expected vuln-scan to fail for vulnerable package, but it passed")
	}
	if !strings.Contains(string(out), "CVE-2026-1234") {
		t.Errorf("expected vulnerability ID in output, got: %s", string(out))
	}
}
