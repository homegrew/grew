// Package validation provides utilities for validating package names, versions,
// and cryptographic checksums.
package validation

import (
	"encoding/hex"
	"fmt"
	"regexp"
)

// SafeNameRe is a regular expression that defines the allowed format for package names.
// Names must start with an alphanumeric character and can contain alphanumeric
// characters, dots, underscores, hyphens, and plus signs.
var SafeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9@._\-\+]*$`)

// SafeVersionRe is a regular expression that defines the allowed format for package versions.
// Versions must start with an alphanumeric character and can contain alphanumeric
// characters, dots, underscores, hyphens, plus signs, tildes, and colons.
var SafeVersionRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._\-\+\~:]*$`)

// IsValidName reports whether the given name matches the safe name criteria.
func IsValidName(name string) bool {
	return SafeNameRe.MatchString(name)
}

// IsValidVersion reports whether the given version matches the safe version criteria.
func IsValidVersion(version string) bool {
	return SafeVersionRe.MatchString(version)
}

// ValidateSHA256 checks that s is a valid 64-character hex-encoded SHA256 string.
// It returns an error if the string length is incorrect or if it contains invalid hex characters.
func ValidateSHA256(s string) error {
	if len(s) != 64 {
		return fmt.Errorf("must be 64 hex characters, got %d", len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return fmt.Errorf("invalid hex: %w", err)
	}
	return nil
}
