package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/safepath"
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
	DownloadSHA512 string `json:"download_sha512,omitempty"`

	// Aggregate integrity hashes.
	KegSHA256 string `json:"keg_sha256"`
	KegSHA512 string `json:"keg_sha512,omitempty"`

	// Per-file inventory, sorted by path.
	Files []FileEntry `json:"files"`

	// Symlinks created by the linker (opt, bin, lib, include).
	Links []LinkEntry `json:"links,omitempty"`

	// Formula dependency names at install time.
	Dependencies []string `json:"dependencies,omitempty"`

	// Install reason: "request" if installed directly, "dependency" if pulled in.
	InstalledOnRequest bool `json:"installed_on_request,omitempty"`

	// Build method: true if built from source, false if poured from a bottle.
	BuiltFromSource bool `json:"built_from_source,omitempty"`
}

// FileEntry records one file or symlink inside the keg.
type FileEntry struct {
	Path    string      `json:"path"`             // relative to keg root
	SHA256  string      `json:"sha256,omitempty"` // empty for dirs/symlinks
	SHA512  string      `json:"sha512,omitempty"` // empty for dirs/symlinks
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
	Platform           string
	DownloadURL        string
	DownloadSHA256     string
	DownloadSHA512     string
	Dependencies       []string
	InstalledOnRequest bool
	BuiltFromSource    bool
}

// Save atomically writes the manifest to kegPath/.MANIFEST.json.
func Save(m *Manifest, kegPath string) error {
	kegPath, err := safepath.CleanPath(kegPath)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')

	dest := filepath.Join(kegPath, ManifestFile)
	if err := fsutil.WriteFileAtomic(dest, data, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// Load reads and parses the manifest from kegPath/.MANIFEST.json.
func Load(kegPath string) (*Manifest, error) {
	kegPath, err := safepath.CleanPath(kegPath)
	if err != nil {
		return nil, err
	}
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
	kegPath, err := safepath.CleanPath(kegPath)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(kegPath, ManifestFile))
	return err == nil
}

// Now returns the current UTC time formatted for manifests.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
