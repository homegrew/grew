package linkage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadCache(t *testing.T) {
	dir := t.TempDir()
	r := &Result{
		Name:    "foo",
		Version: "1.0",
		KegPath: dir,
		Binaries: []BinaryResult{
			{
				Path: "/bin/foo",
				Deps: []Dep{
					{Path: "/usr/lib/libSystem.B.dylib", Kind: System, Resolved: "/usr/lib/libSystem.B.dylib"},
					{Path: "/opt/cellar/bar/1.0/lib/libbar.dylib", Kind: OtherKeg, Resolved: "/opt/cellar/bar/1.0/lib/libbar.dylib", Formula: "bar"},
					{Path: "/missing/lib.dylib", Kind: Broken},
				},
			},
		},
	}

	if err := SaveCache(r); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	loaded, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadCache returned nil")
	}

	if loaded.Name != r.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, r.Name)
	}
	if loaded.Version != r.Version {
		t.Errorf("Version = %q, want %q", loaded.Version, r.Version)
	}
	if loaded.KegPath != r.KegPath {
		t.Errorf("KegPath = %q, want %q", loaded.KegPath, r.KegPath)
	}
	if len(loaded.Binaries) != 1 {
		t.Fatalf("len(Binaries) = %d, want 1", len(loaded.Binaries))
	}
	if len(loaded.Binaries[0].Deps) != 3 {
		t.Fatalf("len(Deps) = %d, want 3", len(loaded.Binaries[0].Deps))
	}

	dep := loaded.Binaries[0].Deps[1]
	if dep.Kind != OtherKeg {
		t.Errorf("Deps[1].Kind = %d, want %d (OtherKeg)", dep.Kind, OtherKeg)
	}
	if dep.Formula != "bar" {
		t.Errorf("Deps[1].Formula = %q, want %q", dep.Formula, "bar")
	}
}

func TestLoadCache_NoFile(t *testing.T) {
	dir := t.TempDir()
	r, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if r != nil {
		t.Fatalf("expected nil result, got %+v", r)
	}
}

func TestLoadCache_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cacheFile), []byte("not json{{{"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := LoadCache(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if r != nil {
		t.Fatalf("expected nil result, got %+v", r)
	}
}

func TestSaveCache_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	r := &Result{
		Name:    "test",
		Version: "2.0",
		KegPath: dir,
	}

	if err := SaveCache(r); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	path := CachePath(dir)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cache file does not exist: %v", err)
	}
	if info.Size() == 0 {
		t.Error("cache file is empty")
	}

	// Verify temp file was cleaned up.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file was not cleaned up")
	}
}
