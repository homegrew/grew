package validation

import (
	"encoding/hex"
	"fmt"
	"regexp"
)

var SafeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9@_-]*$`)
var SafeVersionRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func IsValidName(name string) bool {
	return SafeNameRe.MatchString(name)
}

func IsValidVersion(version string) bool {
	return SafeVersionRe.MatchString(version)
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
