package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/fsutil"
)

func TestVerifySHA256_Match(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello world")
	path := filepath.Join(tmpDir, "testfile")
	os.WriteFile(path, content, 0644)

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	if err := VerifySHA256(path, expected); err != nil {
		t.Fatalf("expected match, got error: %v", err)
	}
}

func TestVerifySHA256_Mismatch(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "testfile")
	os.WriteFile(path, []byte("hello world"), 0644)

	err := VerifySHA256(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

// HTTPS is enforced at the formula layer (formula.GetURL rejects HTTP).
// The downloader itself doesn't duplicate that check because the formula
// validator is the single source of truth for URL policy.
// See internal/formula/formula.go TestParse_HTTPURLRejected and TestGetURL_RejectsHTTP.

func TestDownload_TLS(t *testing.T) {
	content := []byte("test binary content")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "19")
		w.Write(content)
	}))
	defer server.Close()

	// Use the test server's TLS client
	origTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = origTransport }()

	tmpDir := t.TempDir()
	dl := &Downloader{TmpDir: tmpDir}
	path, err := dl.Download(server.URL+"/test", "testpkg-1.0")
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content = %q, want %q", string(data), string(content))
	}
}

func TestDownload_HTTPError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	origTransport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	defer func() { http.DefaultTransport = origTransport }()

	tmpDir := t.TempDir()
	dl := &Downloader{TmpDir: tmpDir}
	_, err := dl.Download(server.URL+"/missing", "testpkg")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestExtract_Binary(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "mybinary")
	os.WriteFile(srcFile, []byte("#!/bin/sh\necho hello\n"), 0755)

	destDir := filepath.Join(tmpDir, "dest")
	spec := formula.InstallSpec{Type: "binary", BinaryName: "myapp"}

	if err := Extract(srcFile, destDir, spec); err != nil {
		t.Fatalf("extract binary failed: %v", err)
	}

	binPath := filepath.Join(destDir, "bin", "myapp")
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("binary not found: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Error("binary should be executable")
	}
}

func TestStripPath(t *testing.T) {
	tests := []struct {
		name  string
		strip int
		want  string
	}{
		{"foo/bar/baz.txt", 0, "foo/bar/baz.txt"},
		{"foo/bar/baz.txt", 1, "bar/baz.txt"},
		{"foo/bar/baz.txt", 2, "baz.txt"},
		{"foo/bar/baz.txt", 3, ""},
		{"foo/", 1, ""},
	}
	for _, tt := range tests {
		got := stripPath(tt.name, tt.strip)
		if got != tt.want {
			t.Errorf("stripPath(%q, %d) = %q, want %q", tt.name, tt.strip, got, tt.want)
		}
	}
}

func TestWithinDir(t *testing.T) {
	if !withinDir("/tmp/dest", "/tmp/dest/foo/bar") {
		t.Error("expected /tmp/dest/foo/bar within /tmp/dest")
	}
	if withinDir("/tmp/dest", "/tmp/other") {
		t.Error("expected /tmp/other NOT within /tmp/dest")
	}
	if withinDir("/tmp/dest", "/tmp/dest/../other") {
		t.Error("expected traversal path NOT within /tmp/dest")
	}
}

func TestSanitizeMode(t *testing.T) {
	// Setuid should be stripped
	mode := fsutil.SanitizeMode(os.ModeSetuid|0755, false)
	if mode&os.ModeSetuid != 0 {
		t.Error("setuid bit should be stripped")
	}
	// World-write should be stripped
	mode = fsutil.SanitizeMode(0777, false)
	if mode&0002 != 0 {
		t.Error("world-write bit should be stripped")
	}
	// Zero mode file gets 0644
	mode = fsutil.SanitizeMode(0, false)
	if mode != 0644 {
		t.Errorf("zero mode file = %o, want 0644", mode)
	}
	// Zero mode dir gets 0755
	mode = fsutil.SanitizeMode(0, true)
	if mode != 0755 {
		t.Errorf("zero mode dir = %o, want 0755", mode)
	}
}
