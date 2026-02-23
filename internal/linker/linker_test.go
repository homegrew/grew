package linker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/config"
)

func setupTestLinker(t *testing.T) (*Linker, config.Paths) {
	t.Helper()
	tmpDir := t.TempDir()
	paths := config.Paths{
		Root:    tmpDir,
		Cellar:  filepath.Join(tmpDir, "Cellar"),
		Opt:     filepath.Join(tmpDir, "opt"),
		Bin:     filepath.Join(tmpDir, "bin"),
		Lib:     filepath.Join(tmpDir, "lib"),
		Include: filepath.Join(tmpDir, "include"),
	}
	for _, d := range []string{paths.Cellar, paths.Opt, paths.Bin, paths.Lib, paths.Include} {
		os.MkdirAll(d, 0755)
	}
	return &Linker{Paths: paths}, paths
}

func createTestKeg(t *testing.T, cellarPath, name, version string) {
	t.Helper()
	kegBin := filepath.Join(cellarPath, name, version, "bin")
	os.MkdirAll(kegBin, 0755)
	os.WriteFile(filepath.Join(kegBin, name), []byte("binary"), 0755)
}

func TestLink_CreatesSymlinks(t *testing.T) {
	lnk, paths := setupTestLinker(t)
	createTestKeg(t, paths.Cellar, "mypkg", "1.0.0")

	if err := lnk.Link("mypkg", "1.0.0", false); err != nil {
		t.Fatalf("link failed: %v", err)
	}

	// Check opt symlink
	optLink := filepath.Join(paths.Opt, "mypkg")
	target, err := os.Readlink(optLink)
	if err != nil {
		t.Fatalf("opt symlink not created: %v", err)
	}
	expected := filepath.Join(paths.Cellar, "mypkg", "1.0.0")
	if target != expected {
		t.Errorf("opt symlink target = %q, want %q", target, expected)
	}

	// Check bin symlink
	binLink := filepath.Join(paths.Bin, "mypkg")
	target, err = os.Readlink(binLink)
	if err != nil {
		t.Fatalf("bin symlink not created: %v", err)
	}
	expectedBin := filepath.Join(paths.Cellar, "mypkg", "1.0.0", "bin", "mypkg")
	if target != expectedBin {
		t.Errorf("bin symlink target = %q, want %q", target, expectedBin)
	}
}

func TestLink_KegOnly(t *testing.T) {
	lnk, paths := setupTestLinker(t)
	createTestKeg(t, paths.Cellar, "mypkg", "1.0.0")

	if err := lnk.Link("mypkg", "1.0.0", true); err != nil {
		t.Fatalf("link failed: %v", err)
	}

	// opt symlink should exist
	if _, err := os.Readlink(filepath.Join(paths.Opt, "mypkg")); err != nil {
		t.Fatal("opt symlink should exist for keg-only")
	}

	// bin symlink should NOT exist
	if _, err := os.Readlink(filepath.Join(paths.Bin, "mypkg")); err == nil {
		t.Fatal("bin symlink should NOT exist for keg-only")
	}
}

func TestUnlink_RemovesSymlinks(t *testing.T) {
	lnk, paths := setupTestLinker(t)
	createTestKeg(t, paths.Cellar, "mypkg", "1.0.0")
	lnk.Link("mypkg", "1.0.0", false)

	if err := lnk.Unlink("mypkg"); err != nil {
		t.Fatalf("unlink failed: %v", err)
	}

	if _, err := os.Readlink(filepath.Join(paths.Opt, "mypkg")); err == nil {
		t.Error("opt symlink should be removed")
	}
	if _, err := os.Readlink(filepath.Join(paths.Bin, "mypkg")); err == nil {
		t.Error("bin symlink should be removed")
	}
}

func TestIsLinked(t *testing.T) {
	lnk, paths := setupTestLinker(t)
	createTestKeg(t, paths.Cellar, "mypkg", "1.0.0")

	if lnk.IsLinked("mypkg") {
		t.Fatal("should not be linked yet")
	}

	lnk.Link("mypkg", "1.0.0", false)

	if !lnk.IsLinked("mypkg") {
		t.Fatal("should be linked")
	}
}

func TestLink_ConflictDetection(t *testing.T) {
	lnk, paths := setupTestLinker(t)

	// Create a regular file where the symlink would go
	os.WriteFile(filepath.Join(paths.Bin, "mypkg"), []byte("existing"), 0644)

	createTestKeg(t, paths.Cellar, "mypkg", "1.0.0")

	err := lnk.Link("mypkg", "1.0.0", false)
	if err == nil {
		t.Fatal("expected conflict error")
	}
}
