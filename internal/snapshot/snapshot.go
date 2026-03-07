package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ManifestFile is the name of the manifest stored inside each keg.
const ManifestFile = ".MANIFEST.json"

// Manifest records the complete state of an installed keg at the time of
// installation. It enables integrity verification and reproducibility.
type Manifest struct {
	// Identity
	Name    string `json:"name"`
	Version string `json:"version"`

	// Provenance
	Platform       string `json:"platform"`
	InstalledAt    string `json:"installed_at"`
	DownloadURL    string `json:"download_url"`
	DownloadSHA256 string `json:"download_sha256"`

	// Aggregate integrity hash (SHA-256 of all file hashes concatenated in order).
	KegSHA256 string `json:"keg_sha256"`

	// Per-file inventory, sorted by path.
	Files []FileEntry `json:"files"`

	// Symlinks created by the linker (opt, bin, lib, include).
	Links []LinkEntry `json:"links,omitempty"`

	// Formula dependency names at install time.
	Dependencies []string `json:"dependencies,omitempty"`
}

// FileEntry records one file or symlink inside the keg.
type FileEntry struct {
	Path    string      `json:"path"`              // relative to keg root
	SHA256  string      `json:"sha256,omitempty"`  // empty for dirs/symlinks
	Size    int64       `json:"size"`
	Mode    os.FileMode `json:"mode"`
	Symlink string      `json:"symlink,omitempty"` // target if symlink
}

// LinkEntry records a symlink created outside the keg (in bin/, opt/, etc.).
type LinkEntry struct {
	Src    string `json:"src"`    // relative to grew root (e.g. "bin/jq")
	Target string `json:"target"` // absolute path inside cellar
}

// InstallMeta carries provenance data from the install command into Capture.
type InstallMeta struct {
	Platform       string
	DownloadURL    string
	DownloadSHA256 string
	Dependencies   []string
}

// Save atomically writes the manifest to kegPath/.MANIFEST.json.
func Save(m *Manifest, kegPath string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')

	dest := filepath.Join(kegPath, ManifestFile)
	dir := filepath.Dir(dest)

	tmp, err := os.CreateTemp(dir, ".manifest-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp manifest: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dest)
}

// Load reads and parses the manifest from kegPath/.MANIFEST.json.
func Load(kegPath string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(kegPath, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// Exists returns true if a manifest exists for the given keg.
func Exists(kegPath string) bool {
	_, err := os.Stat(filepath.Join(kegPath, ManifestFile))
	return err == nil
}

// Now returns the current UTC time formatted for manifests.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
