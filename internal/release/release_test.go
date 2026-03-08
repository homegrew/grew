package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetName(t *testing.T) {
	t.Parallel()
	name := AssetName()
	if !strings.HasPrefix(name, "grew_") {
		t.Errorf("expected prefix grew_, got %s", name)
	}
	if !strings.HasSuffix(name, ".tar.gz") {
		t.Errorf("expected suffix .tar.gz, got %s", name)
	}
}

func TestFindAssetURL(t *testing.T) {
	t.Parallel()
	rel := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "grew_Darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin.tar.gz"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		},
	}

	t.Run("found", func(t *testing.T) {
		t.Parallel()
		url, err := FindAssetURL(rel, "checksums.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://example.com/checksums.txt" {
			t.Errorf("got %s", url)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		_, err := FindAssetURL(rel, "nonexistent.tar.gz")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got: %v", err)
		}
	})

	t.Run("non-HTTPS rejected", func(t *testing.T) {
		t.Parallel()
		badRel := &Release{
			TagName: "v1.0.0",
			Assets:  []Asset{{Name: "bad", BrowserDownloadURL: "http://evil.com/bad"}},
		}
		_, err := FindAssetURL(badRel, "bad")
		if err == nil {
			t.Fatal("expected error for non-HTTPS URL")
		}
		if !strings.Contains(err.Error(), "non-HTTPS") {
			t.Errorf("expected 'non-HTTPS' in error, got: %v", err)
		}
	})
}

func TestFindChecksum(t *testing.T) {
	t.Parallel()
	data := []byte(`# checksums
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  grew_Darwin_arm64.tar.gz
abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890  grew_Linux_x86_64.tar.gz
`)

	t.Run("found", func(t *testing.T) {
		t.Parallel()
		hash, err := FindChecksum(data, "grew_Darwin_arm64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
			t.Errorf("wrong hash: %s", hash)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		_, err := FindChecksum(data, "grew_Windows_amd64.tar.gz")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("skips comments and blanks", func(t *testing.T) {
		t.Parallel()
		data := []byte("\n# comment\n\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  target.tar.gz\n")
		hash, err := FindChecksum(data, "target.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
			t.Errorf("wrong hash: %s", hash)
		}
	})

	t.Run("invalid hash length", func(t *testing.T) {
		t.Parallel()
		data := []byte("shorthash  target.tar.gz\n")
		_, err := FindChecksum(data, "target.tar.gz")
		if err == nil {
			t.Fatal("expected error for short hash")
		}
	})

	t.Run("path prefix stripped", func(t *testing.T) {
		t.Parallel()
		data := []byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  ./dist/target.tar.gz\n")
		hash, err := FindChecksum(data, "target.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash == "" {
			t.Error("expected non-empty hash")
		}
	})
}

func TestExtractBinary(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()
		data := makeTarGz(t, []testEntry{
			{name: "grew", content: "binary", typeflag: tar.TypeReg},
		})
		got, err := ExtractBinary(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "binary" {
			t.Errorf("got %q, want %q", got, "binary")
		}
	})

	t.Run("nested", func(t *testing.T) {
		t.Parallel()
		data := makeTarGz(t, []testEntry{
			{name: "grew-1.0/grew", content: "nested", typeflag: tar.TypeReg},
		})
		got, err := ExtractBinary(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "nested" {
			t.Errorf("got %q, want %q", got, "nested")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		data := makeTarGz(t, []testEntry{
			{name: "other", content: "x", typeflag: tar.TypeReg},
		})
		_, err := ExtractBinary(bytes.NewReader(data))
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("skips symlinks", func(t *testing.T) {
		t.Parallel()
		data := makeTarGz(t, []testEntry{
			{name: "grew", typeflag: tar.TypeSymlink, linkTarget: "/evil"},
			{name: "real/grew", content: "real", typeflag: tar.TypeReg},
		})
		got, err := ExtractBinary(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "real" {
			t.Errorf("got %q, want %q", got, "real")
		}
	})

	t.Run("rejects traversal", func(t *testing.T) {
		t.Parallel()
		data := makeTarGz(t, []testEntry{
			{name: "../../../grew", content: "evil", typeflag: tar.TypeReg},
			{name: "safe/grew", content: "safe", typeflag: tar.TypeReg},
		})
		got, err := ExtractBinary(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "safe" {
			t.Errorf("got %q, want %q", got, "safe")
		}
	})

	t.Run("rejects absolute path", func(t *testing.T) {
		t.Parallel()
		data := makeTarGz(t, []testEntry{
			{name: "/usr/bin/grew", content: "evil", typeflag: tar.TypeReg},
			{name: "ok/grew", content: "ok", typeflag: tar.TypeReg},
		})
		got, err := ExtractBinary(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != "ok" {
			t.Errorf("got %q, want %q", got, "ok")
		}
	})
}

func TestFileSHA256(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := []byte("hello world\n")
	os.WriteFile(path, content, 0644)

	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestFileSHA256_NotExist(t *testing.T) {
	t.Parallel()
	_, err := FileSHA256("/nonexistent/file")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestAtomicInstall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dst := filepath.Join(dir, "grew")

	data := []byte("binary content")
	if err := AtomicInstall(dst, data); err != nil {
		t.Fatalf("AtomicInstall: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "binary content" {
		t.Errorf("got %q, want %q", got, "binary content")
	}

	info, _ := os.Stat(dst)
	if info.Mode().Perm() != 0755 {
		t.Errorf("mode = %o, want 0755", info.Mode().Perm())
	}
}

func TestAtomicInstall_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dst := filepath.Join(dir, "grew")

	os.WriteFile(dst, []byte("old"), 0644)

	if err := AtomicInstall(dst, []byte("new")); err != nil {
		t.Fatalf("AtomicInstall: %v", err)
	}

	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestHttpsGet_RejectsHTTP(t *testing.T) {
	t.Parallel()
	_, err := httpsGet("http://example.com", "text/plain")
	if err == nil {
		t.Fatal("expected error for HTTP URL")
	}
	if !strings.Contains(err.Error(), "refusing non-HTTPS") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- test helpers ---

type testEntry struct {
	name       string
	content    string
	typeflag   byte
	linkTarget string
}

func makeTarGz(t *testing.T, entries []testEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Size:     int64(len(e.content)),
			Mode:     0755,
			Typeflag: e.typeflag,
		}
		if e.typeflag == tar.TypeSymlink {
			hdr.Linkname = e.linkTarget
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}

	tw.Close()
	gw.Close()
	return buf.Bytes()
}
