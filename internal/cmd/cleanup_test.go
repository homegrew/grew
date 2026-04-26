package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/cellar"
)

func TestBelongsToTargets(t *testing.T) {
	targets := []string{"jq", "nmap"}
	tests := []struct {
		filename string
		want     bool
	}{
		{"jq-1.7.1.bottle.tar.gz", true},
		{"nmap-7.94.bottle.tar.gz", true},
		{"gcc-13.2.0.bottle.tar.gz", false},
		{"jq", false}, // must have hyphen
		{"j-q-1.0.tar.gz", false},
	}

	for _, tt := range tests {
		if got := belongsToTargets(targets, tt.filename); got != tt.want {
			t.Errorf("belongsToTargets(%v, %q) = %v, want %v", targets, tt.filename, got, tt.want)
		}
	}
}

func TestIsLatestInstalled(t *testing.T) {
	installed := []cellar.InstalledPackage{
		{Name: "jq", Version: "1.7.1"},
		{Name: "nmap", Version: "7.94"},
	}

	tests := []struct {
		filename string
		want     bool
	}{
		{"jq-1.7.1.bottle.tar.gz", true},
		{"jq-1.7.0.bottle.tar.gz", false},
		{"nmap-7.94.bottle.tar.gz", true},
		{"nmap-7.93.bottle.tar.gz", false},
		{"gcc-13.2.0.bottle.tar.gz", false},
	}

	for _, tt := range tests {
		if got := isLatestInstalled(installed, tt.filename); got != tt.want {
			t.Errorf("isLatestInstalled(%q) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1536, "1.5 KB"},
	}

	for _, tt := range tests {
		if got := formatSize(tt.bytes); got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestDirSize(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some files
	f1 := filepath.Join(tmpDir, "f1")
	if err := os.WriteFile(f1, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	f2 := filepath.Join(subDir, "f2")
	if err := os.WriteFile(f2, []byte("world!"), 0644); err != nil {
		t.Fatal(err)
	}

	// 5 bytes ("hello") + 6 bytes ("world!") = 11 bytes
	want := int64(11)
	got, err := dirSize(tmpDir)
	if err != nil {
		t.Fatalf("dirSize failed: %v", err)
	}
	if got != want {
		t.Errorf("dirSize = %d, want %d", got, want)
	}
}

func TestEntrySize(t *testing.T) {
	tmpDir := t.TempDir()

	// Test file
	f1 := filepath.Join(tmpDir, "f1")
	if err := os.WriteFile(f1, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Check file entry
	got, err := entrySize(f1, entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Errorf("entrySize(file) = %d, want 4", got)
	}

	// Check dir entry
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "f2"), []byte("123"), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err = os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	var dirEntry os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirEntry = e
			break
		}
	}

	got, err = entrySize(subDir, dirEntry)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("entrySize(dir) = %d, want 3", got)
	}
}
