package cellar

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestCellar(t *testing.T) (*Cellar, string) {
	t.Helper()
	tmpDir := t.TempDir()
	// Resolve symlinks (e.g. /var -> /private/var on macOS) so that
	// safepath checks pass consistently.
	resolved, err := filepath.EvalSymlinks(tmpDir)
	if err == nil {
		tmpDir = resolved
	}
	cellarPath := filepath.Join(tmpDir, "Cellar")
	os.MkdirAll(cellarPath, 0755)
	return &Cellar{Path: cellarPath}, tmpDir
}

func createStagingDir(t *testing.T, tmpDir string) string {
	t.Helper()
	stage := filepath.Join(tmpDir, "stage")
	binDir := filepath.Join(stage, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "mybin"), []byte("#!/bin/sh\necho hello\n"), 0755)
	return stage
}

func TestInstall(t *testing.T) {
	cel, tmpDir := setupTestCellar(t)
	stage := createStagingDir(t, tmpDir)

	if err := cel.Install("mypkg", "1.0.0", stage); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	kegPath := filepath.Join(cel.Path, "mypkg", "1.0.0")
	if _, err := os.Stat(kegPath); os.IsNotExist(err) {
		t.Fatal("keg directory not created")
	}

	binPath := filepath.Join(kegPath, "bin", "mybin")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Fatal("binary not installed in keg")
	}
}

func TestIsInstalled(t *testing.T) {
	cel, tmpDir := setupTestCellar(t)
	stage := createStagingDir(t, tmpDir)

	if cel.IsInstalled("mypkg") {
		t.Fatal("mypkg should not be installed yet")
	}

	cel.Install("mypkg", "1.0.0", stage)

	if !cel.IsInstalled("mypkg") {
		t.Fatal("mypkg should be installed")
	}
}

func TestInstalledVersion(t *testing.T) {
	cel, tmpDir := setupTestCellar(t)
	stage := createStagingDir(t, tmpDir)
	cel.Install("mypkg", "2.5.0", stage)

	ver, err := cel.InstalledVersion("mypkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "2.5.0" {
		t.Errorf("version = %q, want %q", ver, "2.5.0")
	}
}

func TestUninstall(t *testing.T) {
	cel, tmpDir := setupTestCellar(t)
	stage := createStagingDir(t, tmpDir)
	cel.Install("mypkg", "1.0.0", stage)

	if err := cel.Uninstall("mypkg"); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}

	if cel.IsInstalled("mypkg") {
		t.Fatal("mypkg should not be installed after uninstall")
	}
}

func TestUninstall_NotInstalled(t *testing.T) {
	cel, _ := setupTestCellar(t)
	if err := cel.Uninstall("nonexistent"); err == nil {
		t.Fatal("expected error for uninstalling non-existent formula")
	}
}

func TestUninstallVersion(t *testing.T) {
	cel, tmpDir := setupTestCellar(t)
	stage := createStagingDir(t, tmpDir)

	// Install two versions
	cel.Install("mypkg", "1.0.0", stage)
	cel.Install("mypkg", "1.1.0", stage)

	if !cel.IsInstalled("mypkg") {
		t.Fatal("mypkg should be installed")
	}

	// Uninstall one version
	if err := cel.UninstallVersion("mypkg", "1.0.0"); err != nil {
		t.Fatalf("uninstall version failed: %v", err)
	}

	if !cel.IsInstalled("mypkg") {
		t.Fatal("mypkg should still be installed (1.1.0 remains)")
	}

	// Uninstall the other version
	if err := cel.UninstallVersion("mypkg", "1.1.0"); err != nil {
		t.Fatalf("uninstall version failed: %v", err)
	}

	if cel.IsInstalled("mypkg") {
		t.Fatal("mypkg should not be installed anymore")
	}

	// Ensure the parent directory is gone
	d := filepath.Join(cel.Path, "mypkg")
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Error("parent formula directory should have been removed")
	}
}

func TestList(t *testing.T) {
	cel, tmpDir := setupTestCellar(t)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		stage := filepath.Join(tmpDir, name+"-stage")
		os.MkdirAll(filepath.Join(stage, "bin"), 0755)
		os.WriteFile(filepath.Join(stage, "bin", name), []byte("test"), 0755)
		cel.Install(name, "1.0.0", stage)
	}

	packages, err := cel.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(packages) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(packages))
	}
	// Should be sorted alphabetically
	if packages[0].Name != "alpha" {
		t.Errorf("first package = %q, want %q", packages[0].Name, "alpha")
	}
}

func TestList_Empty(t *testing.T) {
	cel, _ := setupTestCellar(t)
	packages, err := cel.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(packages) != 0 {
		t.Errorf("expected 0 packages, got %d", len(packages))
	}
}

// TestList_SymlinkedEntrySkipped verifies that List() does not include a cellar
// entry that is a symlink pointing outside the cellar directory.
func TestList_SymlinkedEntrySkipped(t *testing.T) {
	t.Parallel()

	cel, cellarDir := setupTestCellar(t)

	// Install one real formula so we can confirm List() still works.
	stage := filepath.Join(t.TempDir(), "real-stage")
	os.MkdirAll(filepath.Join(stage, "bin"), 0755)
	os.WriteFile(filepath.Join(stage, "bin", "real"), []byte("test"), 0755)
	if err := cel.Install("real", "1.0.0", stage); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Add a symlink inside the cellar directory pointing to an outside directory.
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(outside, "1.0.0"), 0755) // looks like a keg
	if err := os.Symlink(outside, filepath.Join(cellarDir, "evillink")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	packages, err := cel.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, p := range packages {
		if p.Name == "evillink" {
			t.Errorf("List() included symlinked entry %q that escapes the cellar", p.Name)
		}
	}
	// The real formula must still appear.
	found := false
	for _, p := range packages {
		if p.Name == "real" {
			found = true
		}
	}
	if !found {
		t.Error("List() did not include legitimate installed formula")
	}
}
