package cask

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// makeCaskroom creates a temp dir and returns a Caskroom pointing to it.
func makeCaskroom(t *testing.T) (*Caskroom, string) {
	t.Helper()
	dir := t.TempDir()
	return &Caskroom{Path: dir}, dir
}

// installCask creates the directory structure Caskroom/<name>/<version>/.
func installCask(t *testing.T, caskroomDir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(caskroomDir, name, version), 0755); err != nil {
		t.Fatalf("installCask: %v", err)
	}
}

// TestCaskroomList_Empty verifies that an empty caskroom returns nil, not an error.
func TestCaskroomList_Empty(t *testing.T) {
	t.Parallel()
	cr, _ := makeCaskroom(t)
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

// TestCaskroomList_NonExistentPath verifies that a missing directory returns nil, nil.
func TestCaskroomList_NonExistentPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cr := &Caskroom{Path: filepath.Join(dir, "does-not-exist")}
	got, err := cr.List()
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil slice for missing dir, got %v", got)
	}
}

// TestCaskroomList_SingleCask verifies a single installed cask is returned.
func TestCaskroomList_SingleCask(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	installCask(t, dir, "firefox", "120.0")
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cask, got %d: %v", len(got), got)
	}
	if got[0].Name != "firefox" {
		t.Errorf("name = %q, want %q", got[0].Name, "firefox")
	}
	if got[0].Version != "120.0" {
		t.Errorf("version = %q, want %q", got[0].Version, "120.0")
	}
}

// TestCaskroomList_MultipleCasksSorted verifies results are returned in sorted order.
func TestCaskroomList_MultipleCasksSorted(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	installCask(t, dir, "zoom", "5.16.0")
	installCask(t, dir, "alfred", "5.1.1")
	installCask(t, dir, "iterm2", "3.4.22")

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 casks, got %d: %v", len(got), got)
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Name < got[j].Name }) {
		t.Errorf("results not sorted: %v", got)
	}
	if got[0].Name != "alfred" || got[1].Name != "iterm2" || got[2].Name != "zoom" {
		t.Errorf("unexpected order: %v", got)
	}
}

// TestCaskroomList_InvalidNameEntriesFiltered verifies directory entries with
// invalid names (e.g. ".git", "__pycache__", "..", "Invalid") are skipped.
func TestCaskroomList_InvalidNameEntriesFiltered(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	// A valid cask entry.
	installCask(t, dir, "vlc", "3.0.20")
	// Entries that should be ignored because their names fail IsValidName.
	for _, bad := range []string{".hidden", "Has_UPPER", "with space"} {
		if err := os.MkdirAll(filepath.Join(dir, bad), 0755); err != nil {
			t.Fatalf("mkdir %q: %v", bad, err)
		}
	}

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only 1 valid cask, got %d: %v", len(got), got)
	}
	if got[0].Name != "vlc" {
		t.Errorf("name = %q, want %q", got[0].Name, "vlc")
	}
}

// TestCaskroomList_FilesIgnored verifies regular files (non-directories) in the
// caskroom are not returned as casks.
func TestCaskroomList_FilesIgnored(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	installCask(t, dir, "kitty", "0.31.0")
	// Place a regular file that would otherwise pass IsValidName.
	if err := os.WriteFile(filepath.Join(dir, "notadir"), []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "kitty" {
		t.Errorf("expected only kitty, got %v", got)
	}
}

// TestCaskroomList_SymlinkResolution verifies that a caskroom path provided as
// a symlink is resolved to its real target and listing still works correctly.
func TestCaskroomList_SymlinkResolution(t *testing.T) {
	t.Parallel()
	realDir := t.TempDir()
	installCask(t, realDir, "slack", "4.36.140")

	// Create a symlink that points to the real caskroom directory.
	linkDir := filepath.Join(t.TempDir(), "caskroom-link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	cr := &Caskroom{Path: linkDir}
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error after symlink resolution: %v", err)
	}
	if len(got) != 1 || got[0].Name != "slack" {
		t.Errorf("expected [slack], got %v", got)
	}
}

// TestCaskroomList_InvalidInitialPath verifies that a relative path (which
// SafeAbsolutePath rejects) returns an error before any filesystem access.
func TestCaskroomList_InvalidInitialPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: "relative/path"}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for relative caskroom path, got nil")
	}
}

// TestCaskroomList_EmptyPath verifies that an empty path string is rejected.
func TestCaskroomList_EmptyPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: ""}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for empty caskroom path, got nil")
	}
}

// TestCaskroomList_RootPath verifies that the filesystem root "/" is rejected
// by SafeAbsolutePath to prevent accidental enumeration of the entire root.
func TestCaskroomList_RootPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: "/"}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for root path '/', got nil")
	}
}

// TestCaskroomList_DotDotInPath verifies that a path containing ".." traversal
// components is rejected by SafeAbsolutePath.
func TestCaskroomList_DotDotInPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: "/tmp/foo/../bar"}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for path with '..' traversal, got nil")
	}
}

// TestCaskroomList_CaskWithNoVersion verifies that a cask directory that
// contains no valid version subdirectory is silently skipped.
func TestCaskroomList_CaskWithNoVersion(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	// Create the cask directory but give it no version subdirectory.
	if err := os.MkdirAll(filepath.Join(dir, "emptyapp"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A properly installed cask to confirm the valid one is still returned.
	installCask(t, dir, "gpg-suite", "2023.3")

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// emptyapp should be silently skipped; gpg-suite must appear.
	if len(got) != 1 || got[0].Name != "gpg-suite" {
		t.Errorf("expected [gpg-suite], got %v", got)
	}
}

// TestCaskroomList_NestedSymlinkResolution verifies that a caskroom path
// provided through a chain of symlinks (link -> link -> real dir) is fully
// resolved by filepath.EvalSymlinks and listing still works correctly.
func TestCaskroomList_NestedSymlinkResolution(t *testing.T) {
	t.Parallel()
	realDir := t.TempDir()
	installCask(t, realDir, "rectangle", "0.77")

	base := t.TempDir()
	// First hop: link1 -> realDir
	link1 := filepath.Join(base, "link1")
	if err := os.Symlink(realDir, link1); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}
	// Second hop: link2 -> link1
	link2 := filepath.Join(base, "link2")
	if err := os.Symlink(link1, link2); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	cr := &Caskroom{Path: link2}
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error after nested symlink resolution: %v", err)
	}
	if len(got) != 1 || got[0].Name != "rectangle" {
		t.Errorf("expected [rectangle], got %v", got)
	}
}

// TestCaskroomList_SymlinkToInvalidTarget verifies that when a caskroom symlink
// resolves to a path that SafeAbsolutePath would reject (e.g. "/"), List
// returns an error instead of silently proceeding.
func TestCaskroomList_SymlinkToInvalidTarget(t *testing.T) {
	t.Parallel()
	// Create a symlink that points to "/".
	linkDir := filepath.Join(t.TempDir(), "bad-link")
	if err := os.Symlink("/", linkDir); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	cr := &Caskroom{Path: linkDir}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error when resolved symlink target is '/', got nil")
	}
}