package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Capture walks the keg directory and builds a complete manifest.
// It hashes every regular file and records symlink targets.
// The manifest file itself (.MANIFEST.json) is excluded from the inventory.
func Capture(name, version, kegPath string, meta InstallMeta) (*Manifest, error) {
	var files []FileEntry

	err := filepath.Walk(kegPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(kegPath, path)
		if err != nil {
			return err
		}

		// Skip the manifest file itself and the keg root directory entry.
		if rel == "." || rel == ManifestFile {
			return nil
		}

		entry := FileEntry{
			Path: rel,
			Size: info.Size(),
			Mode: info.Mode(),
		}

		// Check for symlink via Lstat (Walk follows symlinks).
		linfo, lerr := os.Lstat(path)
		if lerr != nil {
			return lerr
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.Symlink = target
			entry.Size = 0
			files = append(files, entry)
			return nil
		}

		if info.IsDir() {
			files = append(files, entry)
			return nil
		}

		// Hash regular file.
		h, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		entry.SHA256 = h
		files = append(files, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk keg %s: %w", kegPath, err)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	m := &Manifest{
		Name:           name,
		Version:        version,
		Platform:       meta.Platform,
		InstalledAt:    Now(),
		DownloadURL:    meta.DownloadURL,
		DownloadSHA256: meta.DownloadSHA256,
		KegSHA256:      aggregateHash(files),
		Files:          files,
		Dependencies:   meta.Dependencies,
	}
	return m, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func aggregateHash(files []FileEntry) string {
	h := sha256.New()
	for _, f := range files {
		if f.SHA256 != "" {
			h.Write([]byte(f.SHA256))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
