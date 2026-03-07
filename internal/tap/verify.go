package tap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// VerifyMode controls how strictly tap commit signatures are checked.
type VerifyMode int

const (
	// VerifyOff disables signature verification (default).
	VerifyOff VerifyMode = iota
	// VerifyWarn logs a warning if the commit is unsigned but continues.
	VerifyWarn
	// VerifyStrict refuses to use a tap whose HEAD commit is not signed.
	VerifyStrict
)

// ParseVerifyMode converts a string (from env or config) to a VerifyMode.
// Accepted values: "off", "warn", "strict". Defaults to VerifyOff.
func ParseVerifyMode(s string) VerifyMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "warn":
		return VerifyWarn
	case "strict":
		return VerifyStrict
	default:
		return VerifyOff
	}
}

// TapVerifyMode returns the verification mode from the environment.
// Set HOMEGREW_TAP_VERIFY=warn or HOMEGREW_TAP_VERIFY=strict to enable.
func TapVerifyMode() VerifyMode {
	return ParseVerifyMode(os.Getenv("HOMEGREW_TAP_VERIFY"))
}

// VerifyHeadSignature checks whether the HEAD commit of the git repository
// at repoDir has a valid GPG/SSH signature.
//
// It runs `git verify-commit HEAD` and checks the exit code.
// Returns nil if the commit is signed and valid, or an error describing
// the failure.
//
// Prerequisites:
//   - The tap must be a git clone (not API-fetched tarballs).
//   - The signing key must be in the user's GPG/SSH allowed signers.
func VerifyHeadSignature(repoDir string) error {
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("not a git repository: %s", repoDir)
	}

	cmd := exec.Command("git", "-C", repoDir, "verify-commit", "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("commit signature verification failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// VerifyTagSignature checks whether the given tag in the git repository
// at repoDir has a valid GPG/SSH signature.
func VerifyTagSignature(repoDir, tag string) error {
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("not a git repository: %s", repoDir)
	}

	cmd := exec.Command("git", "-C", repoDir, "verify-tag", tag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tag signature verification failed for %s: %s", tag, strings.TrimSpace(string(output)))
	}
	return nil
}

// CheckAfterUpdate verifies the tap commit signature according to the
// current verification mode. Returns an error only in strict mode when
// verification fails. In warn mode, prints a warning to stderr.
func CheckAfterUpdate(repoDir string, mode VerifyMode) error {
	if mode == VerifyOff {
		return nil
	}

	err := VerifyHeadSignature(repoDir)
	if err == nil {
		return nil
	}

	switch mode {
	case VerifyWarn:
		fmt.Fprintf(os.Stderr, "Warning: tap commit is not signed: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Set HOMEGREW_TAP_VERIFY=strict to enforce signature verification.\n")
		return nil
	case VerifyStrict:
		return fmt.Errorf("refusing unsigned tap update: %w\n"+
			"  The HEAD commit of %s is not signed.\n"+
			"  Set HOMEGREW_TAP_VERIFY=off to disable (not recommended).", err, repoDir)
	}
	return nil
}
