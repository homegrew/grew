package sudo

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/homegrew/grew/pkg/safepath"
)

// The AppleScript payload used to prompt for a password.
const askPassScript = `#!/usr/bin/env osascript
on run
    set thePrompt to "grew requires elevated privileges to install a package."
    try
        set theResult to display dialog thePrompt default answer "" with title "grew" with hidden answer
        return text returned of theResult
    on error
        error number -128
    end try
end run
`

// RunSudoCmd executes the given executable under sudo, using a graphical
// password prompt on macOS via SUDO_ASKPASS.
//
// It writes a temporary AppleScript helper to os.TempDir(), sets
// SUDO_ASKPASS to point at it, then invokes /usr/bin/sudo -A. The helper
// is removed on return. Stdout and stderr are forwarded to the calling
// process.
//
// executable must be an absolute path to a regular file; the function
// validates this before spawning anything. All additional args are passed
// after a -- separator so they cannot be misinterpreted as sudo flags.
func RunSudoCmd(executable string, args ...string) error {
	if executable == "" {
		return fmt.Errorf("no executable provided")
	}

	if err := safepath.SafeAbsolutePath(executable); err != nil {
		return fmt.Errorf("invalid executable path: %w", err)
	}

	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("executable does not exist: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("executable is not a regular file")
	}

	sudoPath := "/usr/bin/sudo"
	cmdArgs := []string{"-A", "--", executable}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command(sudoPath, cmdArgs...)

	// Pass through stdout/stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Create the temporary askpass script
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, "grew_askpass")

	if err := os.WriteFile(scriptPath, []byte(askPassScript), 0700); err != nil {
		return fmt.Errorf("failed to write askpass script: %w", err)
	}
	defer func() {
		if err := os.Remove(scriptPath); err != nil {
			slog.Debug("failed to remove askpass script", "error", err)
		}
	}()

	// Set the environment variable for sudo
	cmd.Env = append(os.Environ(), fmt.Sprintf("SUDO_ASKPASS=%s", scriptPath))

	return cmd.Run()
}
