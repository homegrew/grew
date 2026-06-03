package quarantine

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RunScript executes an embedded Swift script by writing it to a temporary file.
func RunScript(scriptContent []byte, args ...string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "grew-swift-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir for swift script: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "script.swift")
	if err := os.WriteFile(scriptPath, scriptContent, 0700); err != nil {
		return "", fmt.Errorf("write swift script: %w", err)
	}

	cmdArgs := append([]string{scriptPath}, args...)
	cmd := exec.Command("/usr/bin/swift", cmdArgs...)
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("swift script failed: %w (stderr: %s)", err, stderr.String())
	}

	return stdout.String(), nil
}
