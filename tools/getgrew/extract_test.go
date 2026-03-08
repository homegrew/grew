package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/release"
)

func createTestTarGz(t *testing.T, entries []testTarEntry) []byte {
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

type testTarEntry struct {
	name       string
	content    string
	typeflag   byte
	linkTarget string
}

func TestExtractBinary_Basic(t *testing.T) {
	t.Parallel()
	data := createTestTarGz(t, []testTarEntry{
		{name: "grew", content: "binary-content", typeflag: tar.TypeReg},
	})
	got, err := release.ExtractBinary(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "binary-content" {
		t.Errorf("got %q, want %q", got, "binary-content")
	}
}

func TestExtractBinary_Nested(t *testing.T) {
	t.Parallel()
	data := createTestTarGz(t, []testTarEntry{
		{name: "grew-1.0/grew", content: "nested-binary", typeflag: tar.TypeReg},
	})
	got, err := release.ExtractBinary(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "nested-binary" {
		t.Errorf("got %q, want %q", got, "nested-binary")
	}
}

func TestExtractBinary_NotFound(t *testing.T) {
	t.Parallel()
	data := createTestTarGz(t, []testTarEntry{
		{name: "other-tool", content: "nope", typeflag: tar.TypeReg},
	})
	_, err := release.ExtractBinary(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error for missing grew binary")
	}
}

func TestExtractBinary_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	data := createTestTarGz(t, []testTarEntry{
		{name: "grew", typeflag: tar.TypeSymlink, linkTarget: "/etc/passwd"},
		{name: "real/grew", content: "real-binary", typeflag: tar.TypeReg},
	})
	got, err := release.ExtractBinary(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "real-binary" {
		t.Errorf("got %q, want %q — symlink should have been skipped", got, "real-binary")
	}
}

func TestExtractBinary_RejectsTraversal(t *testing.T) {
	t.Parallel()
	data := createTestTarGz(t, []testTarEntry{
		{name: "../../../tmp/grew", content: "evil", typeflag: tar.TypeReg},
		{name: "safe/grew", content: "safe-binary", typeflag: tar.TypeReg},
	})
	got, err := release.ExtractBinary(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "safe-binary" {
		t.Errorf("got %q, want %q — traversal entry should have been skipped", got, "safe-binary")
	}
}

func TestFindChecksum(t *testing.T) {
	t.Parallel()
	checksums := []byte(`# checksums for grew v1.0.0
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  grew_1.0.0_linux_amd64.tar.gz
abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890  grew_1.0.0_darwin_arm64.tar.gz
`)

	tests := []struct {
		name      string
		asset     string
		wantHash  string
		wantError bool
	}{
		{"linux amd64", "grew_1.0.0_linux_amd64.tar.gz", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", false},
		{"darwin arm64", "grew_1.0.0_darwin_arm64.tar.gz", "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", false},
		{"not found", "grew_1.0.0_windows_amd64.tar.gz", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := release.FindChecksum(checksums, tt.asset)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantHash {
				t.Errorf("got %q, want %q", got, tt.wantHash)
			}
		})
	}
}

func TestAssetName(t *testing.T) {
	t.Parallel()
	name := release.AssetName()
	if name == "" {
		t.Fatal("AssetName returned empty string")
	}
	if !strings.HasPrefix(name, "grew_") || !strings.HasSuffix(name, ".tar.gz") {
		t.Errorf("unexpected asset name format: %s", name)
	}
}
