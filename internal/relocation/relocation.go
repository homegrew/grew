// Package relocation rewrites hardcoded library paths inside Mach-O and
// ELF binaries after a bottle is poured into the Cellar. On macOS it
// shells out to otool/install_name_tool; on Linux it uses patchelf.
//
// Homebrew bottles embed placeholder strings (@@HOMEBREW_PREFIX@@,
// @@HOMEBREW_CELLAR@@) instead of real paths. Relocation replaces these
// placeholders — and any real foreign prefix paths — with the local
// grew prefix.
package relocation

import (
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
	standardPrefixes := []string{"/opt/homebrew", "/usr/local"}
	for _, sp := range standardPrefixes {
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

// RelocateKeg walks all binaries in kegPath and rewrites any hardcoded
// paths from placeholder/foreign prefixes to the local prefix.
// Returns an error if relocation could not be completed.
func RelocateKeg(kegPath, prefix string) error {
	slog.Debug(fmt.Sprintf("relocation: scanning keg %s (target prefix %s)", kegPath, prefix))

	replacements := BuildReplacements(kegPath, prefix)

	// Check if any binary actually needs relocation.
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
		if !isBinary(path) {
			return nil
		}

		relPath, _ := filepath.Rel(kegPath, path)
		slog.Debug(fmt.Sprintf("relocation: processing binary %s", relPath))
		if relErr := relocateBinary(path, replacements); relErr != nil {
			slog.Warn(fmt.Sprintf("relocation: %s: %v", relPath, relErr))
			failed++
		} else {
			relocated++
		}
		return nil
	})

	slog.Info(fmt.Sprintf("relocation: %d binaries relocated, %d failed", relocated, failed))

	if failed > 0 {
		return fmt.Errorf("relocation failed for %d binary(ies)", failed)
	}
	return err
}

// needsRelocation checks if any binary in the keg contains paths that
// need rewriting.
func needsRelocation(kegPath string, replacements Replacements) bool {
	found := false
	filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !isBinary(path) {
			return nil
		}
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
			return filepath.SkipAll
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
				if filepath.IsAbs(candidate) {
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

	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}

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

	if magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}

	return false
}
