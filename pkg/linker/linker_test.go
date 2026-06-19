package linker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/pkg/config"
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
		Share:   filepath.Join(tmpDir, "share"),
	}
	for _, d := range []string{paths.Cellar, paths.Opt, paths.Bin, paths.Lib, paths.Include, paths.Share} {
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

	// Create a share/terminfo directory in the keg to trigger the exception
	terminfoDir := filepath.Join(paths.Cellar, "mypkg", "1.0.0", "share", "terminfo", "x")
	os.MkdirAll(terminfoDir, 0755)
	os.WriteFile(filepath.Join(terminfoDir, "xterm-256color"), []byte("data"), 0644)

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

	// share/terminfo/x/xterm-256color symlink SHOULD exist
	terminfoLink := filepath.Join(paths.Share, "terminfo", "x", "xterm-256color")
	if _, err := os.Readlink(terminfoLink); err != nil {
		t.Fatalf("share/terminfo symlink should exist for keg-only formulas: %v", err)
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

func TestLink_VersionFamilyConflict(t *testing.T) {
	lnk, paths := setupTestLinker(t)

	// "node" is installed non-keg-only and owns bin/node.
	createTestKeg(t, paths.Cellar, "node", "20.0.0")
	if err := lnk.Link("node", "20.0.0", false); err != nil {
		t.Fatalf("link node failed: %v", err)
	}

	// Linking node@24 (same family, different keg) must be refused.
	createTestKeg(t, paths.Cellar, "node@24", "24.0.0")
	err := lnk.LinkWithOpts("node@24", "24.0.0", LinkOpts{})
	if err == nil {
		t.Fatal("expected version-family conflict error linking node@24 over node")
	}
	if !strings.Contains(err.Error(), "same version family") {
		t.Fatalf("error %q does not mention version family", err.Error())
	}

	// With Overwrite set, the link must succeed.
	if err := lnk.LinkWithOpts("node@24", "24.0.0", LinkOpts{Overwrite: true}); err != nil {
		t.Fatalf("overwrite link of node@24 should succeed: %v", err)
	}
}

func TestLink_VersionFamilyNoConflictWhenOtherKegOnly(t *testing.T) {
	lnk, paths := setupTestLinker(t)

	// "node" is installed keg-only: it has opt/node but no bin links.
	createTestKeg(t, paths.Cellar, "node", "20.0.0")
	if err := lnk.LinkWithOpts("node", "20.0.0", LinkOpts{KegOnly: true}); err != nil {
		t.Fatalf("keg-only link node failed: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(paths.Opt, "node")); err != nil {
		t.Fatal("opt/node should exist for keg-only node")
	}

	// node@24 is a different family member but node owns no bin links, so there
	// is no real competition for bin/ — linking must succeed.
	createTestKeg(t, paths.Cellar, "node@24", "24.0.0")
	if err := lnk.LinkWithOpts("node@24", "24.0.0", LinkOpts{}); err != nil {
		t.Fatalf("linking node@24 should not conflict with keg-only node: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(paths.Bin, "node@24")); err != nil {
		t.Fatalf("bin/node@24 should be linked: %v", err)
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
