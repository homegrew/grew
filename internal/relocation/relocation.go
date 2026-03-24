// Package relocation rewrites hardcoded library paths inside Mach-O and
// ELF binaries after a bottle is poured into the Cellar. On macOS it
// shells out to otool/install_name_tool; on Linux it uses patchelf.
package relocation

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// RelocateKeg walks all binaries in kegPath and rewrites any hardcoded
// paths from the CI build prefix to the local prefix. Relocation errors
// are logged as warnings and do not fail the install.
func RelocateKeg(kegPath, prefix string) error {
	oldPrefix := detectOldPrefix(kegPath)
	if oldPrefix == "" {
		slog.Debug("no relocatable paths found in keg")
		return nil
	}
	if oldPrefix == prefix {
		slog.Debug("keg already uses local prefix, skipping relocation")
		return nil
	}

	slog.Info(fmt.Sprintf("relocating %s -> %s", oldPrefix, prefix))

	return filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		// Validate the path stays within the keg.
		if rel, relErr := filepath.Rel(kegPath, path); relErr != nil || strings.HasPrefix(rel, "..") {
			return nil
		}

		if isBinary(path) {
			if relErr := relocateBinary(path, oldPrefix, prefix); relErr != nil {
				slog.Warn(fmt.Sprintf("relocate %s: %v", filepath.Base(path), relErr))
			}
		}
		return nil
	})
}

// detectOldPrefix finds the first binary in the keg and inspects its
// embedded paths to discover the CI build prefix. It looks for paths
// containing "/Cellar/" or "/opt/" and extracts the prefix before that
// segment.
func detectOldPrefix(kegPath string) string {
	var oldPrefix string
	filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || oldPrefix != "" {
			return filepath.SkipDir
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !isBinary(path) {
			return nil
		}
		paths, inspectErr := inspectBinary(path)
		if inspectErr != nil {
			return nil // try next binary
		}
		oldPrefix = deriveOldPrefix(paths)
		if oldPrefix != "" {
			return filepath.SkipAll
		}
		return nil
	})
	return oldPrefix
}

// deriveOldPrefix scans a list of embedded paths for patterns like
// "<prefix>/Cellar/" or "<prefix>/opt/" and returns the prefix portion.
func deriveOldPrefix(paths []string) string {
	markers := []string{"/Cellar/", "/opt/", "/lib/"}
	for _, p := range paths {
		for _, marker := range markers {
			if idx := strings.Index(p, marker); idx > 0 {
				candidate := p[:idx]
				// Sanity check: must be absolute.
				if filepath.IsAbs(candidate) {
					return candidate
				}
			}
		}
	}
	return ""
}

// isBinary checks the file magic bytes to determine if path is a
// Mach-O or ELF binary.
func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}

	// Mach-O: CF FA ED FE (64-bit LE), FE ED FA CF (64-bit BE),
	//         CE FA ED FE (32-bit LE), FE ED FA CE (32-bit BE),
	//         CA FE BA BE (universal/fat binary)
	switch {
	case magic[0] == 0xCF && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE:
		return true
	case magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCF:
		return true
	case magic[0] == 0xCE && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE:
		return true
	case magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCE:
		return true
	case magic[0] == 0xCA && magic[1] == 0xFE && magic[2] == 0xBA && magic[3] == 0xBE:
		return true
	}

	// ELF: 7F 45 4C 46
	if magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}

	return false
}
