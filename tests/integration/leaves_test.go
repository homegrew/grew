//go:build integration

package integration

import (
	"github.com/homegrew/grew/tests/testhelper"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLeaves(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	archiveData1 := []byte("fake-binary-data")
	archiveSHA256_1 := testhelper.ComputeSHA256(archiveData1)
	archiveData2 := []byte("fake-binary-data-2")
	archiveSHA256_2 := testhelper.ComputeSHA256(archiveData2)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "dep1.tar.gz") {
			w.Write(archiveData1)
		} else if strings.Contains(r.URL.Path, "pkga.tar.gz") {
			w.Write(archiveData2)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	testhelper.WriteServerCert(server, certFile)

	testhelper.CreateFormula(t, prefix, "dep1", `
name: dep1
version: 1.0.0
url:
  `+platformKey()+`: `+server.URL+`/dep1.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA256_1+`
install:
  type: binary
  binary_name: dep1bin
`)

	testhelper.CreateFormula(t, prefix, "pkga", `
name: pkga
version: 2.0.0
url:
  `+platformKey()+`: `+server.URL+`/pkga.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA256_2+`
install:
  type: binary
  binary_name: pkgabin
dependencies:
  - dep1
`)

	env := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(tmpDir, "cache"),
		"HOMEGREW_TEST_CERT_FILE="+certFile,
		"HOMEGREW_ALLOWED_HOSTS=127.0.0.1",
	)

	// Install pkga, which should pull in dep1
	cmd := exec.Command(exePath, "install", "pkga")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to install pkga: %v, output:\n%s", err, string(out))
	}

	// Run leaves command
	cmd = exec.Command(exePath, "leaves")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run leaves: %v, output:\n%s", err, string(out))
	}

	output := strings.TrimSpace(string(out))
	lines := strings.Split(output, "\n")
	
	if len(lines) != 1 || lines[0] != "pkga" {
		t.Errorf("expected only 'pkga' as leaf, got:\n%s", output)
	}

	// Run leaves --installed-on-request
	cmd = exec.Command(exePath, "leaves", "-r")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run leaves -r: %v, output:\n%s", err, string(out))
	}
	output = strings.TrimSpace(string(out))
	lines = strings.Split(output, "\n")
	if len(lines) != 1 || lines[0] != "pkga" {
		t.Errorf("expected 'pkga' for -r, got:\n%s", output)
	}

	// Uninstall pkga so dep1 becomes a leaf (an orphaned dependency)
	cmd = exec.Command(exePath, "uninstall", "pkga")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to uninstall pkga: %v, output:\n%s", err, string(out))
	}

	// Run leaves --installed-as-dependency
	cmd = exec.Command(exePath, "leaves", "-p")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run leaves -p: %v, output:\n%s", err, string(out))
	}
	output = strings.TrimSpace(string(out))
	lines = strings.Split(output, "\n")
	if len(lines) != 1 || lines[0] != "dep1" {
		t.Errorf("expected 'dep1' for -p (orphaned dep), got:\n%s", output)
	}
}

func platformKey() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}
