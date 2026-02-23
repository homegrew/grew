package cellar

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestCellar(t *testing.T) (*Cellar, string) {
	t.Helper()
	tmpDir := t.TempDir()
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
