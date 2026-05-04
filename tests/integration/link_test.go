
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

func TestLinkUnlinkConflict(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := testhelper.SetupPrefix(t, tmpDir)
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	tarballA := makeDummyTarGz(t, "echo A")
	tarballB := makeDummyTarGz(t, "echo B")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), "pkga") {
			w.Write(tarballA)
		} else {
			w.Write(tarballB)
		}
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	testhelper.WriteServerCert(server, certFile)
	serverHost := strings.TrimPrefix(server.URL, "https://")
	if idx := strings.Index(serverHost, ":"); idx != -1 {
		serverHost = serverHost[:idx]
	}

	commonEnv := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"),
		"HOMEGREW_ALLOWED_HOSTS="+serverHost,
		"HOMEGREW_TEST_CERT_FILE="+certFile,
	)

	platform := runtime.GOOS + "_" + runtime.GOARCH

	testhelper.CreateFormula(t, prefix, "pkga", fmt.Sprintf(`name: pkga
version: 1.0.0
bottle:
  %s:
    url: %s/pkga.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, testhelper.ComputeSHA256(tarballA)))

	testhelper.CreateFormula(t, prefix, "pkgb", fmt.Sprintf(`name: pkgb
version: 1.0.0
bottle:
  %s:
    url: %s/pkgb.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, testhelper.ComputeSHA256(tarballB)))

	// 1. Install pkga
	cmdA := exec.Command(exePath, "install", "pkga")
	cmdA.Env = commonEnv
	if out, err := cmdA.CombinedOutput(); err != nil {
		t.Fatalf("failed to install pkga: %v\nOutput:\n%s", err, string(out))
	}

	// 2. Install pkgb - should conflict on bin/dummybin
	cmdB := exec.Command(exePath, "install", "pkgb")
	cmdB.Env = commonEnv
	out, err := cmdB.CombinedOutput()
	if err == nil {
		t.Error("expected conflict error when installing pkgb, but got none")
	}
	if !strings.Contains(string(out), "conflict") && !strings.Contains(string(out), "already exists") && !strings.Contains(string(out), "already linked") {
		t.Errorf("expected conflict error message, got: %s", string(out))
	}

	// 3. Unlink pkga
	cmdUnlink := exec.Command(exePath, "unlink", "pkga")
	cmdUnlink.Env = commonEnv
	if err := cmdUnlink.Run(); err != nil {
		t.Fatalf("failed to unlink pkga: %v", err)
	}

	// 4. Now install pkgb should succeed
	cmdB2 := exec.Command(exePath, "install", "pkgb")
	cmdB2.Env = commonEnv
	if err := cmdB2.Run(); err != nil {
		t.Fatalf("failed to install pkgb after unlinking pkga: %v", err)
	}
}
