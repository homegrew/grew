package tap

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/homegrew/grew/pkg/safepath"
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
//   - The tap must be a git clone.
//   - The signing key must be in the user's GPG/SSH allowed signers.
func VerifyHeadSignature(repoDir string) error {
	var err error
	repoDir, err = cleanDir(repoDir)
	if err != nil {
		return fmt.Errorf("invalid repo dir: %w", err)
	}
	gitDir, err := safepath.SafeJoin(repoDir, ".git")
	if err != nil {
		return fmt.Errorf("invalid git dir path: %w", err)
	}
	if _, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("not a git repository: %s", repoDir)
	}

	cmd := exec.Command("git", "verify-commit", "HEAD")
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("commit signature verification failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// VerifyTagSignature checks whether the given tag in the git repository
// at repoDir has a valid GPG/SSH signature.
func VerifyTagSignature(repoDir, tag string) error {
	var err error
	repoDir, err = cleanDir(repoDir)
	if err != nil {
		return fmt.Errorf("invalid repo dir: %w", err)
	}
	gitDir, err := safepath.SafeJoin(repoDir, ".git")
	if err != nil {
		return fmt.Errorf("invalid git dir path: %w", err)
	}
	if _, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("not a git repository: %s", repoDir)
	}

	// Validate tag to prevent command injection via git arguments.
	if strings.ContainsAny(tag, "/\\.\x00 \t\n") || strings.HasPrefix(tag, "-") {
		return fmt.Errorf("invalid tag name: %q", tag)
	}

	cmd := exec.Command("git", "verify-tag", "--", tag)
	cmd.Dir = repoDir
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
		slog.Warn(fmt.Sprintf("tap commit is not signed: %v", err))
		slog.Warn("set HOMEGREW_TAP_VERIFY=strict to enforce signature verification")
		return nil
	case VerifyStrict:
		return fmt.Errorf("refusing unsigned tap update: %w\n"+
			"  The HEAD commit of %s is not signed.\n"+
			"  Set HOMEGREW_TAP_VERIFY=off to disable (not recommended).", err, repoDir)
	}
	return nil
}
