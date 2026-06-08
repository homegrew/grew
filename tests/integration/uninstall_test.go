package integration

import (
	"archive/tar"
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

	"github.com/homegrew/grew/tests/testhelper"
)

func makeDummyTarGzWithShare(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	dirs := []string{"bin/", "share/", "share/pkgconfig/", "share/man/", "share/man/man1/", "share/info/"}
	for _, d := range dirs {
		if err := tw.WriteHeader(&tar.Header{Name: d, Mode: 0755, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]string{
		"bin/dummybin":           "echo hello",
		"share/pkgconfig/foo.pc": "Name: foo",
		"share/man/man1/foo.1":   "MAN PAGE",
		"share/info/foo.info":    "INFO PAGE",
	}

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Size:     int64(len(content)),
			Mode:     0644,
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func TestUninstallWithShare(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := testhelper.SetupPrefix(t, tmpDir)
	exePath := testhelper.BuildTestBinary(t, tmpDir)

	tarball := makeDummyTarGzWithShare(t)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarball)
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

	testhelper.CreateFormula(t, prefix, "fooshare", fmt.Sprintf(`name: fooshare
version: 1.0.0
bottle:
  %s:
    url: %s/fooshare.tar.gz
    sha256: %s
install:
  type: archive
`, platform, server.URL, testhelper.ComputeSHA256(tarball)))

	// Install
	cmdA := exec.Command(exePath, "install", "fooshare")
	cmdA.Env = commonEnv
	if out, err := cmdA.CombinedOutput(); err != nil {
		t.Fatalf("failed to install fooshare: %v\nOutput:\n%s", err, string(out))
	}

	// Verify links exist
	if _, err := os.Stat(filepath.Join(prefix, "bin", "dummybin")); os.IsNotExist(err) {
		t.Errorf("expected bin/dummybin to be linked")
	}
	if _, err := os.Stat(filepath.Join(prefix, "share", "pkgconfig", "foo.pc")); os.IsNotExist(err) {
		t.Errorf("expected share/pkgconfig/foo.pc to be linked")
	}
	if _, err := os.Stat(filepath.Join(prefix, "share", "man", "man1", "foo.1")); err == nil {
		t.Errorf("expected share/man/man1/foo.1 NOT to be linked (should ignore man)")
	}
	if _, err := os.Stat(filepath.Join(prefix, "share", "info", "foo.info")); err == nil {
		t.Errorf("expected share/info/foo.info NOT to be linked (should ignore info)")
	}

	// Uninstall
	cmdUnlink := exec.Command(exePath, "uninstall", "fooshare")
	cmdUnlink.Env = commonEnv
	if out, err := cmdUnlink.CombinedOutput(); err != nil {
		t.Fatalf("failed to uninstall fooshare: %v\nOutput:\n%s", err, string(out))
	}

	// Verify links are gone
	if _, err := os.Stat(filepath.Join(prefix, "bin", "dummybin")); err == nil {
		t.Errorf("expected bin/dummybin to be removed")
	}
	if _, err := os.Stat(filepath.Join(prefix, "share", "pkgconfig", "foo.pc")); err == nil {
		t.Errorf("expected share/pkgconfig/foo.pc to be removed")
	}
	if _, err := os.Stat(filepath.Join(prefix, "share", "pkgconfig")); err == nil {
		t.Errorf("expected empty dir share/pkgconfig to be removed")
	}
}

// TestUninstallRefusesDependency verifies that uninstalling a formula that is
// still required by another installed formula is refused, and that
// --ignore-dependencies overrides the guard.
func TestUninstallRefusesDependency(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	exePath := testhelper.BuildTestBinary(t, tmpDir)
	prefix := testhelper.SetupPrefix(t, tmpDir)

	archiveData := []byte("fake-binary-data")
	archiveSHA := testhelper.ComputeSHA256(archiveData)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archiveData)
	}))
	defer server.Close()

	certFile := filepath.Join(tmpDir, "server.crt")
	testhelper.WriteServerCert(server, certFile)

	testhelper.CreateFormula(t, prefix, "libfoo", `
name: libfoo
version: 1.0.0
url:
  `+platformKey()+`: `+server.URL+`/libfoo.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
  binary_name: libfoo
`)

	testhelper.CreateFormula(t, prefix, "bar", `
name: bar
version: 1.0.0
dependencies:
  - libfoo
url:
  `+platformKey()+`: `+server.URL+`/bar.tar.gz
sha256:
  `+platformKey()+`: `+archiveSHA+`
install:
  type: binary
  binary_name: bar
`)

	env := append(os.Environ(),
		"HOMEGREW_PREFIX="+prefix,
		"HOMEGREW_CACHE="+filepath.Join(prefix, "cache"),
		"HOMEGREW_TEST_CERT_FILE="+certFile,
		"HOMEGREW_ALLOWED_HOSTS=127.0.0.1,localhost,example.com",
	)

	runGrew := func(args ...string) (string, error) {
		cmd := exec.Command(exePath, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := runGrew("install", "bar"); err != nil {
		t.Fatalf("failed to install bar: %v\nOutput: %s", err, out)
	}

	// if out, err := runGrew("install", "libfoo"); err != nil {
	// 	t.Fatalf("failed to install libfoo: %v\nOutput: %s", err, out)
	// }

	// Attempting to remove libfoo while bar depends on it must fail.
	out, err := runGrew("uninstall", "libfoo")
	if err == nil {
		t.Fatalf("expected uninstall to fail, but it succeeded. Output: %s", out)
	}
	if !strings.Contains(out, "Refusing to uninstall") {
		t.Errorf("expected refusal message, got: %s", out)
	}
	if !strings.Contains(out, "bar") {
		t.Errorf("expected dependent 'bar' to be named in the error, got: %s", out)
	}
	if !strings.Contains(out, "--ignore-dependencies") {
		t.Errorf("expected override hint in the error, got: %s", out)
	}

	// libfoo must still be installed.
	if out, err := runGrew("list"); err != nil || !strings.Contains(out, "libfoo") {
		t.Fatalf("expected libfoo to still be installed after refused uninstall; list: %s", out)
	}

	// --ignore-dependencies must allow the removal.
	if out, err := runGrew("uninstall", "--ignore-dependencies", "libfoo"); err != nil {
		t.Fatalf("uninstall --ignore-dependencies failed: %v\nOutput: %s", err, out)
	}
	if out, err := runGrew("list"); err == nil && strings.Contains(out, "libfoo") {
		t.Fatalf("expected libfoo to be gone after --ignore-dependencies uninstall; list: %s", out)
	}
}
