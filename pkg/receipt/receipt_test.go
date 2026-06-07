package receipt

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReceipt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "receipt-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	kegPath := filepath.Join(tmpDir, "test-keg")
	if err := os.MkdirAll(kegPath, 0755); err != nil {
		t.Fatalf("failed to create keg dir: %v", err)
	}

	// Use RFC3339 precision for comparison since JSON marshal/unmarshal will use it.
	now := time.Now().UTC().Truncate(time.Second)

	r := &Receipt{
		Name:                "test-formula",
		Version:             "1.0.0",
		BuiltFromSource:     true,
		PouredFromBottle:    false,
		InstalledAt:         now,
		Dependencies:        []string{"dep1", "dep2"},
		RuntimeDependencies: []string{"run1"},
		Compiler:            "clang",
		BuildOptions:        []string{"--with-test"},
		InstalledOnRequest:  true,
	}

	// Test Save
	if err := Save(r, kegPath); err != nil {
		t.Fatalf("failed to save receipt: %v", err)
	}

	// Test Exists
	if !Exists(kegPath) {
		t.Errorf("expected receipt to exist")
	}

	// Test Load
	loaded, err := Load(kegPath)
	if err != nil {
		t.Fatalf("failed to load receipt: %v", err)
	}

	if loaded.Name != r.Name {
		t.Errorf("expected name %q, got %q", r.Name, loaded.Name)
	}
	if loaded.Version != r.Version {
		t.Errorf("expected version %q, got %q", r.Version, loaded.Version)
	}
	if !loaded.InstalledAt.Equal(r.InstalledAt) {
		t.Errorf("expected time %v, got %v", r.InstalledAt, loaded.InstalledAt)
	}
	if len(loaded.Dependencies) != len(r.Dependencies) {
		t.Errorf("expected %d dependencies, got %d", len(r.Dependencies), len(loaded.Dependencies))
	}
	if loaded.InstalledOnRequest != r.InstalledOnRequest {
		t.Errorf("expected InstalledOnRequest %v, got %v", r.InstalledOnRequest, loaded.InstalledOnRequest)
	}
}

func TestExists_NonExistent(t *testing.T) {
	if Exists("/tmp/definitely-does-not-exist-grew-receipt") {
		t.Errorf("expected Exists to return false for non-existent path")
	}
}
