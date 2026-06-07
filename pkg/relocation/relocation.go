// Package relocation rewrites hardcoded library paths inside Mach-O and
// ELF binaries after a bottle is poured into the Cellar. On macOS it
// shells out to otool/install_name_tool.
//
// Homebrew bottles embed placeholder strings (@@HOMEBREW_PREFIX@@,
// @@HOMEBREW_CELLAR@@) instead of real paths. Relocation replaces these
// placeholders — and any real foreign prefix paths — with the local
// grew prefix.
package relocation

import (
	"bytes"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Homebrew placeholder strings found in bottles.
const (
	PlaceholderPrefix = "@@HOMEBREW_PREFIX@@"
	PlaceholderCellar = "@@HOMEBREW_CELLAR@@"
)

// Replacements maps old path prefixes/placeholders to new values.
// Each entry is applied to every binary in the keg.
type Replacements map[string]string

// BuildReplacements returns the set of string replacements needed for
// a keg installed at kegPath with the given prefix. It inspects binaries
// to detect Homebrew placeholders and foreign prefixes.
func BuildReplacements(kegPath, prefix string) Replacements {
	cellar := filepath.Join(prefix, "Cellar")
	r := Replacements{}

	// Always include Homebrew placeholder replacements.
	// These are present in every Homebrew bottle.
	r[PlaceholderPrefix] = prefix
	r[PlaceholderCellar] = cellar

	// Include standard Homebrew prefixes as sources for relocation.
	// Many bottles have these hardcoded even if they are relocatable.
	foreignPrefixes := []string{
		"/opt/homebrew",
		"/usr/local",
	}
	for _, sp := range foreignPrefixes {
		if sp != prefix {
			r[sp] = prefix
			r[filepath.Join(sp, "Cellar")] = cellar
		}
	}

	// Also detect any real (non-placeholder) foreign prefix from the binaries.
	if oldPrefix := detectForeignPrefix(kegPath, prefix); oldPrefix != "" {
		oldCellar := filepath.Join(oldPrefix, "Cellar")
		r[oldPrefix] = prefix
		if oldCellar != cellar {
			r[oldCellar] = cellar
		}
	}

	return r
}

// RelocateKeg walks all binaries and text files in kegPath and rewrites any
// hardcoded paths from placeholder/foreign prefixes to the local prefix.
// Returns an error if relocation could not be completed.
func RelocateKeg(kegPath, prefix string) error {
	slog.Debug(fmt.Sprintf("relocation: scanning keg %s (target prefix %s)", kegPath, prefix))

	replacements := BuildReplacements(kegPath, prefix)

	// Check if any file actually needs relocation.
	needed := needsRelocation(kegPath, replacements)
	if !needed {
		slog.Debug("relocation: no relocatable paths found in keg, nothing to do")
		return nil
	}

	slog.Info(fmt.Sprintf("relocation: applying %d replacement(s)", len(replacements)))
	for old, new_ := range replacements {
		slog.Debug(fmt.Sprintf("relocation:   %s -> %s", old, new_))
	}

	var relocated, failed int
	err := filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			slog.Debug(fmt.Sprintf("relocation: skipping inaccessible entry: %v", walkErr))
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if rel, relErr := filepath.Rel(kegPath, path); relErr != nil || strings.HasPrefix(rel, "..") {
			return nil
		}

		if isBinary(path) {
			relPath, _ := filepath.Rel(kegPath, path)
			slog.Debug(fmt.Sprintf("relocation: processing binary %s", relPath))
			if relErr := relocateBinary(path, replacements); relErr != nil {
				slog.Warn(fmt.Sprintf("relocation: %s: %v", relPath, relErr))

				msg := strings.ToLower(relErr.Error())
				isMisidentified := strings.Contains(msg, "not a mach-o") ||
					strings.Contains(msg, "not an elf") ||
					strings.Contains(msg, "invalid elf") ||
					strings.Contains(msg, "too small") ||
					strings.Contains(msg, "exit status 1")

				if !isMisidentified {
					failed++
				}
			} else {
				relocated++
			}
		}
		return nil
	})

	if err == nil {
		if txtErr := relocateTextFiles(kegPath, replacements); txtErr != nil {
			slog.Warn(fmt.Sprintf("relocation: text files: %v", txtErr))
			failed++
		}
	}

	slog.Info(fmt.Sprintf("relocation: binaries and text files processed"))

	if failed > 0 {
		return fmt.Errorf("relocation failed for %d file(s)", failed)
	}
	return err
}

// isTextFile checks if the file should be treated as a text file for relocation.
func isTextFile(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".el", ".elc", ".pc", ".cmake", ".json", ".sh", ".xml", ".cfg", ".conf", ".desktop", ".service", ".plist":
		return true
	}
	return false
}

// relocateTextFiles performs string replacement on all whitelisted text files in a directory.
func relocateTextFiles(kegPath string, replacements Replacements) error {
	var failed int
	err := filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			slog.Debug(fmt.Sprintf("relocation: skipping inaccessible entry: %v", walkErr))
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if isTextFile(path) {
			if err := relocateSingleTextFile(path, replacements); err != nil {
				relPath, _ := filepath.Rel(kegPath, path)
				if relPath == "" {
					relPath = path
				}
				slog.Warn(fmt.Sprintf("relocation: text file %s: %v", relPath, err))
				failed++
			}
		}
		return nil
	})
	if failed > 0 {
		return fmt.Errorf("%d text file(s) failed relocation", failed)
	}
	return err
}

// relocateSingleTextFile handles the relocation of a single text file, ensuring it is writable.
func relocateSingleTextFile(path string, replacements Replacements) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	modified := false
	newContent := content
	for old, new_ := range replacements {
		oldBytes := []byte(old)
		if bytes.Contains(newContent, oldBytes) {
			newContent = bytes.ReplaceAll(newContent, oldBytes, []byte(new_))
			modified = true
		}
	}

	if !modified {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	originalMode := info.Mode()

	// If not writable, chmod it.
	if originalMode&0200 == 0 {
		if err := os.Chmod(path, originalMode|0200); err != nil {
			return fmt.Errorf("make writable: %w", err)
		}
		// Restore permissions after writing.
		defer os.Chmod(path, originalMode)
	}

	return os.WriteFile(path, newContent, originalMode)
}

// needsRelocation checks if any binary or text file in the keg contains paths that
// need rewriting.
func needsRelocation(kegPath string, replacements Replacements) bool {
	found := false
	filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			if found {
				return filepath.SkipAll
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}

		if isBinary(path) {
			paths, inspectErr := inspectBinary(path)
			if inspectErr != nil {
				return nil
			}
			for _, p := range paths {
				for old := range replacements {
					if strings.Contains(p, old) {
						slog.Debug(fmt.Sprintf("relocation: found %q in %s", old, filepath.Base(path)))
						found = true
						return filepath.SkipAll
					}
				}
			}
		} else if isTextFile(path) {
			content, readErr := os.ReadFile(path)
			if readErr == nil {
				for old := range replacements {
					if bytes.Contains(content, []byte(old)) {
						slog.Debug(fmt.Sprintf("relocation: found %q in text file %s", old, filepath.Base(path)))
						found = true
						return filepath.SkipAll
					}
				}
			}
		}
		return nil
	})
	return found
}

// detectForeignPrefix inspects binaries in the keg for absolute paths
// that contain /Cellar/ or /opt/ markers but point to a different prefix
// than the local one. Returns the foreign prefix or empty string.
func detectForeignPrefix(kegPath, localPrefix string) string {
	slog.Debug(fmt.Sprintf("relocation: detecting foreign prefix in %s", kegPath))

	var foreign string
	filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || foreign != "" {
			if foreign != "" {
				return filepath.SkipAll
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !isBinary(path) {
			return nil
		}
		paths, inspectErr := inspectBinary(path)
		if inspectErr != nil {
			return nil
		}
		foreign = deriveOldPrefix(paths)
		if foreign == localPrefix {
			foreign = "" // same prefix, not foreign
		}
		if foreign != "" {
			slog.Debug(fmt.Sprintf("relocation: detected foreign prefix %s from %s", foreign, filepath.Base(path)))
			return filepath.SkipAll
		}
		return nil
	})
	return foreign
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
				// Exclude @@HOMEBREW_PREFIX@@ as it is already handled explicitly.
				if filepath.IsAbs(candidate) && !strings.Contains(candidate, PlaceholderPrefix) {
					return candidate
				}
			}
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

// isBinary checks the file magic bytes to determine if path is a
// Mach-O or ELF binary.
func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var magic [8]byte
	n, err := f.Read(magic[:])
	if err != nil || n < 4 {
		return false
	}

	switch {
	// Mach-O 64-bit
	case magic[0] == 0xCF && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE:
		return true
	case magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCF:
		return true
	// Mach-O 32-bit
	case magic[0] == 0xCE && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE:
		return true
	case magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCE:
		return true
	// Mach-O Fat / Java .class conflict (0xCAFEBABE)
	case magic[0] == 0xCA && magic[1] == 0xFE && magic[2] == 0xBA && magic[3] == 0xBE:
		// Java .class files have magic 0xCAFEBABE, then 2 bytes minor, 2 bytes major.
		// Mach-O Fat binaries have magic 0xCAFEBABE, then 4 bytes big-endian number of architectures.
		// On modern macOS, major version is usually > 40.
		if n < 8 {
			return false
		}
		narchs := uint32(magic[4])<<24 | uint32(magic[5])<<16 | uint32(magic[6])<<8 | uint32(magic[7])
		// Reasonable Mach-O Fat binaries have very few architectures (usually 2, maybe 3-4).
		// Java class files compiled for Java 1.5 have major version 49, minor 0.
		// 0x00 0x00 0x00 0x31 -> 49.
		// So we limit narchs to < 20 to safely avoid Java class files.
		return narchs > 0 && narchs < 20
	case magic[0] == 0xBE && magic[1] == 0xBA && magic[2] == 0xFE && magic[3] == 0xCA:
		return true
	}

	if magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}

	return false
}
