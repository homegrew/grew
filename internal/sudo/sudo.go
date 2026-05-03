package sudo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// RunSudoCmd executes a command using sudo, prompting the user graphically on macOS if needed.
func RunSudoCmd(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command provided")
	}

	cmdArgs := []string{"-A"}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("sudo", cmdArgs...)

	// Pass through stdout/stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if runtime.GOOS == "darwin" {
		// Create the temporary askpass script
		tmpDir := os.TempDir()
		scriptPath := filepath.Join(tmpDir, "grew_askpass")
		
		if err := os.WriteFile(scriptPath, []byte(askPassScript), 0700); err != nil {
			return fmt.Errorf("failed to write askpass script: %w", err)
		}
		defer os.Remove(scriptPath)

		// Set the environment variable for sudo
		cmd.Env = append(os.Environ(), fmt.Sprintf("SUDO_ASKPASS=%s", scriptPath))
	} else {
		// On non-macOS, fallback to standard terminal sudo behavior if possible
		cmd = exec.Command("sudo", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}