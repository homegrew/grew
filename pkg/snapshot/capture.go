package snapshot

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/homegrew/grew/pkg/downloader"
)

// Capture walks the keg directory and builds a complete manifest.
// It hashes every regular file and records symlink targets.
// The manifest file itself (.MANIFEST.json) is excluded from the inventory.
func Capture(name, version, kegPath string, meta InstallMeta) (*Manifest, error) {
	var files []FileEntry

	err := filepath.WalkDir(kegPath, func(path string, d os.DirEntry, err error) error {
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

		info, err := d.Info()
		if err != nil {
			return err
		}

		entry := FileEntry{
			Path: rel,
			Size: info.Size(),
			Mode: info.Mode(),
		}

		// Check for symlink.
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.Symlink = target
			entry.Size = 0
			files = append(files, entry)
			return nil
		}

		if d.IsDir() {
			files = append(files, entry)
			return nil
		}

		// Hash regular file.
		h256, h512, err := downloader.ComputeHashes(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		entry.SHA256 = h256
		entry.SHA512 = h512
		files = append(files, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk keg %s: %w", kegPath, err)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	keg256, keg512 := aggregateHashes(files)

	m := &Manifest{
		Name:               name,
		Version:            version,
		Platform:           meta.Platform,
		InstalledAt:        Now(),
		DownloadURL:        meta.DownloadURL,
		DownloadSHA256:     meta.DownloadSHA256,
		DownloadSHA512:     meta.DownloadSHA512,
		KegSHA256:          keg256,
		KegSHA512:          keg512,
		Files:              files,
		Dependencies:       meta.Dependencies,
		InstalledOnRequest: meta.InstalledOnRequest,
		BuiltFromSource:    meta.BuiltFromSource,
	}
	return m, nil
}

func aggregateHashes(files []FileEntry) (sha256Hash, sha512Hash string) {
	h256 := sha256.New()
	h512 := sha512.New()
	for _, f := range files {
		if f.SHA256 != "" {
			h256.Write([]byte(f.SHA256))
		}
		if f.SHA512 != "" {
			h512.Write([]byte(f.SHA512))
		}
	}
	return hex.EncodeToString(h256.Sum(nil)), hex.EncodeToString(h512.Sum(nil))
}
