package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// VerifyResult holds the outcome of comparing a manifest against the filesystem.
type VerifyResult struct {
	Name     string
	Version  string
	OK       bool
	Missing  []string // files in manifest but not on disk
	Modified []string // files whose hash or mode changed
	Added    []string // files on disk but not in manifest
	Errors   []string // non-fatal errors encountered during verification
}

// Verify loads the manifest from kegPath and compares it against the
// actual filesystem contents. Returns a detailed result.
func Verify(kegPath string) (*VerifyResult, error) {
	m, err := Load(kegPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	result := &VerifyResult{
		Name:    m.Name,
		Version: m.Version,
	}

	// Build a set of expected paths from the manifest.
	expected := make(map[string]FileEntry, len(m.Files))
	for _, f := range m.Files {
		expected[f.Path] = f
	}

	// Walk the keg and check each file.
	seen := make(map[string]bool, len(m.Files))

	err = filepath.Walk(kegPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("walk error: %s: %v", path, err))
			return nil // continue walking
		}

		rel, err := filepath.Rel(kegPath, path)
		if err != nil {
			return nil
		}
		if rel == "." || rel == ManifestFile {
			return nil
		}

		seen[rel] = true

		entry, inManifest := expected[rel]
		if !inManifest {
			result.Added = append(result.Added, rel)
			return nil
		}

		// Check symlinks.
		linfo, lerr := os.Lstat(path)
		if lerr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("lstat %s: %v", rel, lerr))
			return nil
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			if target != entry.Symlink {
				result.Modified = append(result.Modified, fmt.Sprintf("%s (symlink: %q -> %q)", rel, entry.Symlink, target))
			}
			return nil
		}

		// Skip directories — just check existence (already walking into them).
		if info.IsDir() {
			return nil
		}

		// Check regular file hash.
		if entry.SHA256 != "" {
			actualHash, herr := hashFile(path)
			if herr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("hash %s: %v", rel, herr))
				return nil
			}
			if actualHash != entry.SHA256 {
				result.Modified = append(result.Modified, fmt.Sprintf("%s (sha256 mismatch)", rel))
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk keg for verify: %w", err)
	}

	// Check for files in manifest but missing on disk.
	for _, f := range m.Files {
		if !seen[f.Path] {
			result.Missing = append(result.Missing, f.Path)
		}
	}

	sort.Strings(result.Missing)
	sort.Strings(result.Modified)
	sort.Strings(result.Added)

	result.OK = len(result.Missing) == 0 && len(result.Modified) == 0 && len(result.Added) == 0 && len(result.Errors) == 0
	return result, nil
}
