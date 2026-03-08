package validation

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var SafeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9@._\-\+]*$`)
var SafeVersionRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._\-\+\~:]*$`)

func IsValidName(name string) bool {
	return SafeNameRe.MatchString(name)
}

func IsValidVersion(version string) bool {
	return SafeVersionRe.MatchString(version)
}

// SafePathComponent checks that a filename component does not contain
// path separators, ".." traversals, or null bytes. Use this to validate
// any user-supplied string before joining it into a filesystem path.
func SafePathComponent(name string) error {
	if name == "" {
		return fmt.Errorf("empty path component")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("path component contains separator: %q", name)
	}
	if name == ".." || name == "." {
		return fmt.Errorf("path component is a traversal: %q", name)
	}
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("path component contains null byte")
	}
	return nil
}

// SafeJoin joins base and child path components, then verifies the result
// resolves within base. Returns the cleaned joined path or an error if the
// result would escape base (e.g. via ".." traversal or symlinks).
func SafeJoin(base string, components ...string) (string, error) {
	parts := append([]string{base}, components...)
	joined := filepath.Join(parts...)
	cleaned := filepath.Clean(joined)

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve base path: %w", err)
	}
	absJoined, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve joined path: %w", err)
	}

	// The joined path must be equal to or a child of the base.
	if absJoined != absBase && !strings.HasPrefix(absJoined, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes base %q", cleaned, base)
	}
	return cleaned, nil
}

// ValidateSHA256 checks that s is a valid 64-character hex string.
func ValidateSHA256(s string) error {
	if len(s) != 64 {
		return fmt.Errorf("must be 64 hex characters, got %d", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("invalid hex: %w", err)
	}
	return nil
}
