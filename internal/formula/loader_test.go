package formula

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFormula(t *testing.T, dir, name string) {
	t.Helper()
	yaml := `name: ` + name + `
version: "1.0.0"
description: "Test formula ` + name + `"
homepage: "https://example.com"
license: "MIT"
url:
  darwin_arm64: "https://example.com/` + name + `"
  linux_amd64: "https://example.com/` + name + `"
sha256:
  darwin_arm64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  linux_amd64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
install:
  type: binary
  binary_name: ` + name + `
dependencies: []
keg_only: false
`
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadByName_Found(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)
	writeTestFormula(t, tapDir, "mypkg")

	loader := &Loader{TapDir: tmpDir}
	f, err := loader.LoadByName("mypkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != "mypkg" {
		t.Errorf("name = %q, want %q", f.Name, "mypkg")
	}
}

func TestLoadByName_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)

	loader := &Loader{TapDir: tmpDir}
	_, err := loader.LoadByName("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing formula")
	}
}

func TestLoadAll_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()
	tapDir := filepath.Join(tmpDir, "core")
	os.MkdirAll(tapDir, 0755)
	writeTestFormula(t, tapDir, "pkg1")
	writeTestFormula(t, tapDir, "pkg2")
	writeTestFormula(t, tapDir, "pkg3")

	loader := &Loader{TapDir: tmpDir}
	all, err := loader.LoadAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("loaded %d formulas, want 3", len(all))
	}
}
