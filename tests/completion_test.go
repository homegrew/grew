package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellCompletion(t *testing.T) {
	tmpDir := t.TempDir()
	prefix := setupPrefix(t, tmpDir)
	exePath := buildTestBinary(t, tmpDir)

	// According to README, there should be a 'completion' command
	shells := []string{"bash", "zsh", "fish"}

	for _, shell := range shells {
		cmd := exec.Command(exePath, "completion", shell)
		cmd.Env = append(os.Environ(), "HOMEGREW_PREFIX="+prefix)
		out, err := cmd.CombinedOutput()
		
		// If the command is missing, this will fail. 
		// This test serves to confirm implementation or flag missing feature.
		if err != nil {
			t.Errorf("Completion for %s failed or command missing: %v\nOutput: %s", shell, err, string(out))
			continue
		}

		if len(out) < 100 {
			t.Errorf("Completion output for %s seems too short: %s", shell, string(out))
		}
		
		if shell == "bash" && !strings.Contains(string(out), "complete -F") {
			t.Errorf("Bash completion output missing expected 'complete -F'")
		}
	}
}
