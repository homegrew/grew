package tap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const defaultRepoURL = "https://github.com/homegrew/homegrew-taps.git"

type Manager struct {
	TapsDir string
}

// EnsureCloned clones the taps repo if it hasn't been cloned yet.
func (m *Manager) EnsureCloned() error {
	gitDir := filepath.Join(m.TapsDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil // already cloned
	}

	// If TapsDir exists but isn't a git repo (e.g. leftover from embedded era),
	// remove it so the clone can succeed.
	if entries, err := os.ReadDir(m.TapsDir); err == nil && len(entries) > 0 {
		if err := os.RemoveAll(m.TapsDir); err != nil {
			return fmt.Errorf("clear stale taps dir: %w", err)
		}
	}

	fmt.Printf("==> Cloning taps from %s\n", defaultRepoURL)
	cmd := exec.Command("git", "clone", "--depth", "1", defaultRepoURL, m.TapsDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clone taps repo: %w", err)
	}
	return nil
}

// InitCore ensures the core tap is available on disk.
func (m *Manager) InitCore() error {
	return m.EnsureCloned()
}

// InitCask ensures the cask tap is available on disk.
func (m *Manager) InitCask() error {
	return m.EnsureCloned()
}

// Update pulls the latest tap definitions from the remote repository.
func (m *Manager) Update() (int, error) {
	if err := m.EnsureCloned(); err != nil {
		return 0, err
	}

	fmt.Printf("==> Updating taps...\n")
	fetch := exec.Command("git", "-C", m.TapsDir, "fetch", "--depth", "1", "origin", "main")
	fetch.Stdout = os.Stdout
	fetch.Stderr = os.Stderr
	if err := fetch.Run(); err != nil {
		return 0, fmt.Errorf("update taps: %w", err)
	}
	reset := exec.Command("git", "-C", m.TapsDir, "reset", "--hard", "origin/main")
	reset.Stdout = os.Stdout
	reset.Stderr = os.Stderr
	if err := reset.Run(); err != nil {
		return 0, fmt.Errorf("update taps: %w", err)
	}

	// Count formulas available after update.
	count := 0
	for _, sub := range []string{"core", "cask"} {
		dir := filepath.Join(m.TapsDir, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				count++
			}
		}
	}
	return count, nil
}
