
package integration

import (
	"github.com/homegrew/grew/tests/testhelper"
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
