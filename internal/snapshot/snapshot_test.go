package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func createTestKeg(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	keg := filepath.Join(dir, "mypkg", "1.0.0")
	binDir := filepath.Join(keg, "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "mybin"), []byte("#!/bin/sh\necho hello\n"), 0755)
	os.WriteFile(filepath.Join(keg, "README"), []byte("readme content"), 0644)
	return keg
}

func TestCaptureAndSave(t *testing.T) {
	keg := createTestKeg(t)

	meta := InstallMeta{
		Platform:       "darwin_arm64",
		DownloadURL:    "https://example.com/mypkg-1.0.0.tar.gz",
		DownloadSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Dependencies:   []string{"dep1"},
	}

	m, err := Capture("mypkg", "1.0.0", keg, meta)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	if m.Name != "mypkg" {
		t.Errorf("name = %q, want %q", m.Name, "mypkg")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", m.Version, "1.0.0")
	}
	if m.Platform != "darwin_arm64" {
		t.Errorf("platform = %q, want %q", m.Platform, "darwin_arm64")
	}
	if m.KegSHA256 == "" {
		t.Error("keg_sha256 should not be empty")
	}
	if len(m.Files) < 3 { // bin/, bin/mybin, README
		t.Errorf("expected at least 3 file entries, got %d", len(m.Files))
	}

	// Check that bin/mybin has a hash.
	found := false
	for _, f := range m.Files {
		if f.Path == "bin/mybin" {
			found = true
			if f.SHA256 == "" {
				t.Error("bin/mybin should have a SHA256 hash")
			}
			if f.Mode&0111 == 0 {
				t.Error("bin/mybin should be executable")
			}
		}
	}
	if !found {
		t.Error("bin/mybin not found in manifest files")
	}

	// Save and verify it exists.
	if err := Save(m, keg); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if !Exists(keg) {
		t.Error("manifest should exist after save")
	}

	// Load it back.
	loaded, err := Load(keg)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Name != m.Name {
		t.Errorf("loaded name = %q, want %q", loaded.Name, m.Name)
	}
	if loaded.KegSHA256 != m.KegSHA256 {
		t.Errorf("loaded keg hash differs from saved")
	}
}

func TestVerify_Clean(t *testing.T) {
	keg := createTestKeg(t)

	meta := InstallMeta{Platform: "darwin_arm64"}
	m, err := Capture("mypkg", "1.0.0", keg, meta)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if err := Save(m, keg); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := Verify(keg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !result.OK {
		t.Errorf("expected OK, got missing=%v modified=%v added=%v errors=%v",
			result.Missing, result.Modified, result.Added, result.Errors)
	}
}

func TestVerify_Modified(t *testing.T) {
	keg := createTestKeg(t)

	meta := InstallMeta{Platform: "darwin_arm64"}
	m, _ := Capture("mypkg", "1.0.0", keg, meta)
	Save(m, keg)

	// Tamper with a file.
	os.WriteFile(filepath.Join(keg, "bin", "mybin"), []byte("TAMPERED"), 0755)

	result, err := Verify(keg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.OK {
		t.Fatal("expected verification failure after tampering")
	}
	if len(result.Modified) == 0 {
		t.Error("expected modified files list to be non-empty")
	}
}

func TestVerify_Missing(t *testing.T) {
	keg := createTestKeg(t)

	meta := InstallMeta{Platform: "darwin_arm64"}
	m, _ := Capture("mypkg", "1.0.0", keg, meta)
	Save(m, keg)

	// Delete a file.
	os.Remove(filepath.Join(keg, "README"))

	result, err := Verify(keg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.OK {
		t.Fatal("expected verification failure after deletion")
	}
	if len(result.Missing) == 0 {
		t.Error("expected missing files list to be non-empty")
	}
}

func TestVerify_Added(t *testing.T) {
	keg := createTestKeg(t)

	meta := InstallMeta{Platform: "darwin_arm64"}
	m, _ := Capture("mypkg", "1.0.0", keg, meta)
	Save(m, keg)

	// Add an unexpected file.
	os.WriteFile(filepath.Join(keg, "INTRUDER"), []byte("malicious"), 0644)

	result, err := Verify(keg)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if result.OK {
		t.Fatal("expected verification failure after adding file")
	}
	if len(result.Added) == 0 {
		t.Error("expected added files list to be non-empty")
	}
}
