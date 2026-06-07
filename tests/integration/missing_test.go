package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/tests/testhelper"
)

func TestMissing(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	depData := []byte("fake-dep-binary")
	depSHA := testhelper.ComputeSHA256(depData)
	pkgData := []byte("fake-pkg-binary")
	pkgSHA := testhelper.ComputeSHA256(pkgData)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "dep1.tar.gz"):
			w.Write(depData)
		case strings.Contains(r.URL.Path, "pkga.tar.gz"):
			w.Write(pkgData)
		default:
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
  `+platformKey()+`: `+depSHA+`
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
  `+platformKey()+`: `+pkgSHA+`
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

	// Install pkga, which pulls in dep1.
	cmd := exec.Command(exePath, "install", "pkga")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to install pkga: %v, output:\n%s", err, string(out))
	}

	// With all dependencies present, `missing` should report nothing and exit 0.
	cmd = exec.Command(exePath, "missing")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected `missing` to exit 0 when nothing is missing, got %v, output:\n%s", err, string(out))
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("expected no output when nothing is missing, got:\n%s", string(out))
	}

	// Simulate a broken dependency chain by removing dep1's keg.
	if err := os.RemoveAll(filepath.Join(prefix, "Cellar", "dep1")); err != nil {
		t.Fatalf("failed to remove dep1 keg: %v", err)
	}

	// Now `missing` should report pkga's missing dependency and exit non-zero.
	cmd = exec.Command(exePath, "missing")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected `missing` to exit non-zero when a dependency is missing, output:\n%s", string(out))
	}
	if !strings.Contains(string(out), "pkga: dep1") {
		t.Errorf("expected output to contain %q, got:\n%s", "pkga: dep1", string(out))
	}

	// `--hide` should make an installed dependency appear absent.
	cmd = exec.Command(exePath, "install", "dep1")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to reinstall dep1: %v, output:\n%s", err, string(out))
	}

	cmd = exec.Command(exePath, "missing", "--hide=dep1", "pkga")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected `missing --hide=dep1` to exit non-zero, output:\n%s", string(out))
	}
	if !strings.Contains(string(out), "pkga: dep1") {
		t.Errorf("expected --hide output to contain %q, got:\n%s", "pkga: dep1", string(out))
	}
}
