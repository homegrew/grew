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
	slog.Debug(fmt.Sprintf("relocation: scanning keg %s (target prefix %s)", kegPath, prefix))

	oldPrefix := detectOldPrefix(kegPath)
	if oldPrefix == "" {
		slog.Debug("relocation: no relocatable paths found in keg, nothing to do")
		return nil
	}
	if oldPrefix == prefix {
		slog.Debug(fmt.Sprintf("relocation: keg already uses local prefix %s, skipping", prefix))
		return nil
	}

	slog.Info(fmt.Sprintf("relocating: %s -> %s", oldPrefix, prefix))

	var relocated, skipped int
	err := filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Debug(fmt.Sprintf("relocation: skipping inaccessible entry: %v", err))
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		// Validate the path stays within the keg.
		if rel, relErr := filepath.Rel(kegPath, path); relErr != nil || strings.HasPrefix(rel, "..") {
			slog.Debug(fmt.Sprintf("relocation: skipping path outside keg: %s", path))
			return nil
		}

		if isBinary(path) {
			relPath, _ := filepath.Rel(kegPath, path)
			slog.Debug(fmt.Sprintf("relocation: processing binary %s", relPath))
			if relErr := relocateBinary(path, oldPrefix, prefix); relErr != nil {
				slog.Warn(fmt.Sprintf("relocation: %s: %v", relPath, relErr))
				skipped++
			} else {
				relocated++
			}
		}
		return nil
	})

	slog.Info(fmt.Sprintf("relocation: %d binaries relocated, %d skipped", relocated, skipped))
	return err
}

// Issue represents a relocation problem found during verification.
type Issue struct {
	Binary string // relative path within the keg
	Dep    string // the problematic dependency or path
	Reason string // human-readable description
}

func (i Issue) String() string {
	return fmt.Sprintf("%s: %s (%s)", i.Binary, i.Dep, i.Reason)
}

// VerifyKeg walks all binaries in kegPath and checks that their dynamic
// library dependencies can be resolved. Returns a list of issues found.
// Call this after RelocateKeg and linking to catch broken installs early.
func VerifyKeg(kegPath, prefix string) []Issue {
	slog.Debug(fmt.Sprintf("relocation: verifying keg %s", kegPath))

	var issues []Issue
	filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !isBinary(path) {
			return nil
		}

		relPath, _ := filepath.Rel(kegPath, path)
		binIssues := verifyBinary(path, prefix)
		for _, bi := range binIssues {
			bi.Binary = relPath
			issues = append(issues, bi)
		}
		return nil
	})

	if len(issues) == 0 {
		slog.Debug("relocation: verification passed, all dependencies resolved")
	} else {
		slog.Debug(fmt.Sprintf("relocation: verification found %d issue(s)", len(issues)))
	}
	return issues
}

// detectOldPrefix finds the first binary in the keg and inspects its
// embedded paths to discover the CI build prefix. It looks for paths
// containing "/Cellar/" or "/opt/" and extracts the prefix before that
// segment.
func detectOldPrefix(kegPath string) string {
	slog.Debug(fmt.Sprintf("relocation: detecting old prefix from binaries in %s", kegPath))

	var oldPrefix string
	var toolWarned bool
	var inspected int
	filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries but continue walking
		}
		if oldPrefix != "" {
			return filepath.SkipAll
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !isBinary(path) {
			return nil
		}

		relPath, _ := filepath.Rel(kegPath, path)
		inspected++
		slog.Debug(fmt.Sprintf("relocation: inspecting %s for embedded paths", relPath))

		paths, inspectErr := inspectBinary(path)
		if inspectErr != nil {
			if !toolWarned {
				slog.Warn(fmt.Sprintf("relocation: binary inspection failed, relocation may be skipped: %v", inspectErr))
				toolWarned = true
			}
			return nil // try next binary
		}

		slog.Debug(fmt.Sprintf("relocation: %s has %d embedded paths", relPath, len(paths)))
		for _, p := range paths {
			slog.Debug(fmt.Sprintf("relocation:   %s", p))
		}

		oldPrefix = deriveOldPrefix(paths)
		if oldPrefix != "" {
			slog.Debug(fmt.Sprintf("relocation: detected old prefix %s from %s", oldPrefix, relPath))
			return filepath.SkipAll
		}
		return nil
	})

	if oldPrefix == "" {
		slog.Debug(fmt.Sprintf("relocation: inspected %d binaries, no old prefix found", inspected))
	}
	return oldPrefix
}

// deriveOldPrefix scans a list of embedded paths for patterns like
// "<prefix>/Cellar/" or "<prefix>/opt/" and returns the prefix portion.
func deriveOldPrefix(paths []string) string {
	markers := []string{"/Cellar/", "/opt/"}
	for _, p := range paths {
		for _, marker := range markers {
			idx := strings.Index(p, marker)
			if idx > 0 {
				candidate := p[:idx]
				if filepath.IsAbs(candidate) {
					return candidate
				}
			}
			// The prefix itself may live under /opt (e.g. /opt/grew).
			// When /opt/ matches at position 0, look for a second
			// occurrence: /opt/grew/opt/<name>/... → prefix is /opt/grew.
			if idx == 0 && marker == "/opt/" {
				if idx2 := strings.Index(p[1:], "/opt/"); idx2 > 0 {
					candidate := p[:idx2+1]
					if filepath.IsAbs(candidate) {
						return candidate
					}
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
	//         CA FE BA BE (FAT_MAGIC universal), BE BA FE CA (FAT_CIGAM universal)
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
	case magic[0] == 0xBE && magic[1] == 0xBA && magic[2] == 0xFE && magic[3] == 0xCA:
		return true
	}

	// ELF: 7F 45 4C 46
	if magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}

	return false
}
