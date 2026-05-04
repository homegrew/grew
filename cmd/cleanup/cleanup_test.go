package cleanup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/cmd"
)

func TestBelongsToTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		targets  []string
		filename string
		want     bool
	}{
		{[]string{"jq"}, "jq-1.6.tar.gz", true},
		{[]string{"jq"}, "nmap-7.92.tar.gz", false},
		{[]string{"jq", "nmap"}, "nmap-7.92.tar.gz", true},
		{[]string{}, "any-1.0.tar.gz", false},
	}

	for _, tt := range tests {
		if got := cmd.BelongsToTargets(tt.targets, tt.filename); got != tt.want {
			t.Errorf("belongsToTargets(%v, %q) = %v, want %v", tt.targets, tt.filename, got, tt.want)
		}
	}
}

func TestIsLatestInstalled(t *testing.T) {
	t.Parallel()
	installed := []cellar.InstalledPackage{
		{Name: "jq", Version: "1.6"},
		{Name: "nmap", Version: "7.92"},
	}

	tests := []struct {
		filename string
		want     bool
	}{
		{"jq-1.6.tar.gz", true},
		{"jq-1.5.tar.gz", false},
		{"nmap-7.92-src.tar.gz", true},
		{"other-1.0.tar.gz", false},
	}

	for _, tt := range tests {
		if got := cmd.IsLatestInstalled(installed, tt.filename); got != tt.want {
			t.Errorf("isLatestInstalled(%q) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}

func TestPruneEmptyDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create structure:
	// dir/
	//   empty/
	//   nested/
	//     empty/
	//   non-empty/
	//     file.txt
	
	os.MkdirAll(filepath.Join(dir, "empty"), 0755)
	os.MkdirAll(filepath.Join(dir, "nested", "empty"), 0755)
	os.MkdirAll(filepath.Join(dir, "non-empty"), 0755)
	os.WriteFile(filepath.Join(dir, "non-empty", "file.txt"), []byte("data"), 0644)

	cmd.PruneEmptyDirs(dir)

	if _, err := os.Stat(filepath.Join(dir, "empty")); !os.IsNotExist(err) {
		t.Error("expected empty/ to be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "nested")); !os.IsNotExist(err) {
		t.Error("expected nested/ to be removed because its child was empty")
	}
	if _, err := os.Stat(filepath.Join(dir, "non-empty")); err != nil {
		t.Error("expected non-empty/ to be kept")
	}
}

func TestDirSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	
	file1 := filepath.Join(dir, "file1")
	file2 := filepath.Join(dir, "file2")
	os.WriteFile(file1, []byte("123"), 0644)    // 3 bytes
	os.WriteFile(file2, []byte("12345"), 0644)  // 5 bytes

	size, err := cmd.DirSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if size != 8 {
		t.Errorf("expected size 8, got %d", size)
	}
}

func TestEntrySize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	
	filePath := filepath.Join(dir, "file")
	os.WriteFile(filePath, []byte("1234"), 0644)
	
	entries, _ := os.ReadDir(dir)
	size, err := cmd.EntrySize(filePath, entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if size != 4 {
		t.Errorf("expected size 4, got %d", size)
	}

	// Test directory entry
	subDir := filepath.Join(dir, "subdir")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "f"), []byte("12"), 0644)
	
	entries2, _ := os.ReadDir(dir)
	var subDirEntry os.DirEntry
	for _, e := range entries2 {
		if e.Name() == "subdir" {
			subDirEntry = e
		}
	}
	
	size2, err := cmd.EntrySize(subDir, subDirEntry)
	if err != nil {
		t.Fatal(err)
	}
	if size2 != 2 {
		t.Errorf("expected size 2, got %d", size2)
	}
}
