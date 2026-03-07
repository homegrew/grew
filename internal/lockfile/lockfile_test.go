package lockfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/snapshot"
)

// setupCellar creates a fake cellar with the given packages and optional manifests.
func setupCellar(t *testing.T, packages map[string]struct {
	version  string
	manifest *snapshot.Manifest
}) string {
	t.Helper()
	root := t.TempDir()
	cellarPath := filepath.Join(root, "Cellar")

	for name, pkg := range packages {
		kegPath := filepath.Join(cellarPath, name, pkg.version)
		if err := os.MkdirAll(kegPath, 0755); err != nil {
			t.Fatalf("create keg dir: %v", err)
		}
		if pkg.manifest != nil {
			if err := snapshot.Save(pkg.manifest, kegPath); err != nil {
				t.Fatalf("save manifest: %v", err)
			}
		}
	}

	return root
}

func TestSaveAndLoad(t *testing.T) {
	root := setupCellar(t, map[string]struct {
		version  string
		manifest *snapshot.Manifest
	}{
		"jq": {
			version: "1.7.1",
			manifest: &snapshot.Manifest{
				Name:           "jq",
				Version:        "1.7.1",
				Platform:       "darwin_arm64",
				DownloadURL:    "https://example.com/jq-1.7.1.tar.gz",
				DownloadSHA256: "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234",
				KegSHA256:      "ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01",
				Dependencies:   []string{"oniguruma"},
			},
		},
	})
	cellarPath := filepath.Join(root, "Cellar")

	// Generate.
	lf, err := Generate(root, cellarPath)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(lf.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(lf.Entries))
	}
	entry, ok := lf.Entries["jq"]
	if !ok {
		t.Fatal("missing jq entry")
	}
	if entry.Version != "1.7.1" {
		t.Errorf("version = %q, want 1.7.1", entry.Version)
	}
	if entry.SHA256 != "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234" {
		t.Errorf("unexpected sha256: %s", entry.SHA256)
	}
	if entry.KegSHA256 != "ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01ef01" {
		t.Errorf("unexpected keg_sha256: %s", entry.KegSHA256)
	}

	// Save.
	if err := Save(lf, root); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(LockFilePath(root)); err != nil {
		t.Fatalf("lockfile not found: %v", err)
	}

	// Load.
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("version = %d, want 1", loaded.Version)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry after load, got %d", len(loaded.Entries))
	}
	le := loaded.Entries["jq"]
	if le.Version != entry.Version || le.SHA256 != entry.SHA256 || le.KegSHA256 != entry.KegSHA256 {
		t.Errorf("loaded entry differs from saved: got %+v", le)
	}
	if len(le.Dependencies) != 1 || le.Dependencies[0] != "oniguruma" {
		t.Errorf("dependencies = %v, want [oniguruma]", le.Dependencies)
	}
}

func TestCheck_Clean(t *testing.T) {
	root := setupCellar(t, map[string]struct {
		version  string
		manifest *snapshot.Manifest
	}{
		"curl": {
			version: "8.5.0",
			manifest: &snapshot.Manifest{
				Name:           "curl",
				Version:        "8.5.0",
				Platform:       "darwin_arm64",
				DownloadURL:    "https://example.com/curl-8.5.0.tar.gz",
				DownloadSHA256: "1111111111111111111111111111111111111111111111111111111111111111",
				KegSHA256:      "2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
	})
	cellarPath := filepath.Join(root, "Cellar")

	lf, err := Generate(root, cellarPath)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	discs, err := Check(lf, cellarPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(discs) != 0 {
		t.Errorf("expected 0 discrepancies, got %d: %+v", len(discs), discs)
	}
}

func TestCheck_Missing(t *testing.T) {
	// Create an empty cellar but a lockfile with an entry.
	root := t.TempDir()
	cellarPath := filepath.Join(root, "Cellar")
	if err := os.MkdirAll(cellarPath, 0755); err != nil {
		t.Fatal(err)
	}

	lf := &LockFile{
		Version: 1,
		Entries: map[string]Entry{
			"missing-pkg": {
				Version:  "1.0.0",
				Platform: "darwin_arm64",
			},
		},
	}

	discs, err := Check(lf, cellarPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(discs) != 1 {
		t.Fatalf("expected 1 discrepancy, got %d", len(discs))
	}
	if discs[0].Kind != "missing" {
		t.Errorf("kind = %q, want missing", discs[0].Kind)
	}
	if discs[0].Name != "missing-pkg" {
		t.Errorf("name = %q, want missing-pkg", discs[0].Name)
	}
}

func TestCheck_Extra(t *testing.T) {
	root := setupCellar(t, map[string]struct {
		version  string
		manifest *snapshot.Manifest
	}{
		"extra-pkg": {version: "2.0.0", manifest: nil},
	})
	cellarPath := filepath.Join(root, "Cellar")

	// Empty lockfile.
	lf := &LockFile{
		Version: 1,
		Entries: make(map[string]Entry),
	}

	discs, err := Check(lf, cellarPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(discs) != 1 {
		t.Fatalf("expected 1 discrepancy, got %d", len(discs))
	}
	if discs[0].Kind != "extra" {
		t.Errorf("kind = %q, want extra", discs[0].Kind)
	}
	if discs[0].Name != "extra-pkg" {
		t.Errorf("name = %q, want extra-pkg", discs[0].Name)
	}
}

func TestCheck_VersionMismatch(t *testing.T) {
	root := setupCellar(t, map[string]struct {
		version  string
		manifest *snapshot.Manifest
	}{
		"jq": {version: "1.7.1", manifest: nil},
	})
	cellarPath := filepath.Join(root, "Cellar")

	lf := &LockFile{
		Version: 1,
		Entries: map[string]Entry{
			"jq": {
				Version:  "1.6.0",
				Platform: "darwin_arm64",
			},
		},
	}

	discs, err := Check(lf, cellarPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(discs) != 1 {
		t.Fatalf("expected 1 discrepancy, got %d", len(discs))
	}
	if discs[0].Kind != "version_mismatch" {
		t.Errorf("kind = %q, want version_mismatch", discs[0].Kind)
	}
}

func TestLoadNonexistent(t *testing.T) {
	root := t.TempDir()
	lf, err := Load(root)
	if err != nil {
		t.Fatalf("Load should not error for missing file: %v", err)
	}
	if lf.Version != 1 {
		t.Errorf("version = %d, want 1", lf.Version)
	}
	if len(lf.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(lf.Entries))
	}
}
