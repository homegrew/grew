package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// VerifyResult holds the outcome of comparing a manifest against the filesystem.
type VerifyResult struct {
	Name              string
	Version           string
	OK                bool
	Missing           []string // files in manifest but not on disk
	Modified          []string // files whose hash or mode changed
	Added             []string // files on disk but not in manifest
	Errors            []string // non-fatal errors encountered during verification
	KegSHA256Mismatch bool
	KegSHA512Mismatch bool
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
	var actualFiles []FileEntry

	err = filepath.WalkDir(kegPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("walk error: %s: %v", path, err))
			return nil // continue walking
		}

		rel, err := filepath.Rel(kegPath, path)
		if err != nil {
			return nil
		}
		if rel == "." || rel == ManifestFile || rel == "INSTALL_RECEIPT.json" {
			return nil
		}

		seen[rel] = true

		entry, inManifest := expected[rel]
		if !inManifest {
			result.Added = append(result.Added, rel)
			return nil
		}

		info, err := d.Info()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("stat %s: %v", rel, err))
			return nil
		}

		// Check symlinks.
		if d.Type()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			if target != entry.Symlink {
				result.Modified = append(result.Modified, fmt.Sprintf("%s (symlink: %q -> %q)", rel, entry.Symlink, target))
			}
			actualFiles = append(actualFiles, FileEntry{Path: rel, Symlink: target, Mode: info.Mode()})
			return nil
		}

		// Skip directories — just check existence (already walking into them).
		if d.IsDir() {
			actualFiles = append(actualFiles, FileEntry{Path: rel, Mode: info.Mode()})
			return nil
		}

		// Check regular file hash.
		actual256 := ""
		actual512 := ""
		if entry.SHA256 != "" || entry.SHA512 != "" {
			var herr error
			actual256, actual512, herr = hashFile(path)
			if herr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("hash %s: %v", rel, herr))
				return nil
			}
			if entry.SHA256 != "" && actual256 != entry.SHA256 {
				result.Modified = append(result.Modified, fmt.Sprintf("%s (sha256 mismatch)", rel))
			}
			if entry.SHA512 != "" && actual512 != entry.SHA512 {
				result.Modified = append(result.Modified, fmt.Sprintf("%s (sha512 mismatch)", rel))
			}
		}
		actualFiles = append(actualFiles, FileEntry{
			Path:   rel,
			SHA256: actual256,
			SHA512: actual512,
			Size:   info.Size(),
			Mode:   info.Mode(),
		})

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

	// Check aggregate hashes.
	sort.Slice(actualFiles, func(i, j int) bool { return actualFiles[i].Path < actualFiles[j].Path })
	keg256, keg512 := aggregateHashes(actualFiles)
	if m.KegSHA256 != "" && keg256 != m.KegSHA256 {
		result.KegSHA256Mismatch = true
	}
	if m.KegSHA512 != "" && keg512 != m.KegSHA512 {
		result.KegSHA512Mismatch = true
	}

	result.OK = len(result.Missing) == 0 && len(result.Modified) == 0 && len(result.Added) == 0 &&
		len(result.Errors) == 0 && !result.KegSHA256Mismatch && !result.KegSHA512Mismatch
	return result, nil
}
