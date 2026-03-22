package tap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultRepoURL = "https://github.com/homegrew/homegrew-taps.git"

type Manager struct {
	TapsDir string
}

// cleanDir validates and cleans a directory path to prevent argument
// injection. The result is always a Clean'd absolute path.
func cleanDir(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("empty path")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	// filepath.Abs resolves ".." components, so the result is already
	// traversal-free. Guard against leading dashes that could be
	// interpreted as flags by subprocesses.
	abs = filepath.Clean(abs)
	if strings.HasPrefix(filepath.Base(abs), "-") {
		return "", fmt.Errorf("path component starts with dash: %q", dir)
	}
	return abs, nil
}

// EnsureCloned clones the taps repo if it hasn't been cloned yet.
func (m *Manager) EnsureCloned() error {
	tapsDir, err := cleanDir(m.TapsDir)
	if err != nil {
		return fmt.Errorf("invalid taps dir: %w", err)
	}
	m.TapsDir = tapsDir

	gitDir := filepath.Join(tapsDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil // already cloned
	}

	// If TapsDir exists but isn't a git repo (e.g. leftover from a previous install),
	// remove it so the clone can succeed.
	if entries, err := os.ReadDir(tapsDir); err == nil && len(entries) > 0 {
		if err := os.RemoveAll(tapsDir); err != nil {
			return fmt.Errorf("clear stale taps dir: %w", err)
		}
	}

	fmt.Printf("==> Cloning taps from %s\n", defaultRepoURL)
	cmd := exec.Command("git", "clone", "--depth", "1", "--", defaultRepoURL, tapsDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clone taps repo: %w", err)
	}

	// Verify commit signature if configured.
	if err := CheckAfterUpdate(tapsDir, TapVerifyMode()); err != nil {
		return err
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

// Update pulls the latest tap definitions via git fetch + reset.
// If HOMEGREW_TAP_VERIFY is set to "warn" or "strict", the HEAD commit
// signature is verified after the update.
func (m *Manager) Update() (int, error) {
	if err := m.EnsureCloned(); err != nil {
		return 0, err
	}

	// Use a local validated copy of the taps directory for all path
	// operations so the sanitization is visible at each use site.
	tapsDir, err := cleanDir(m.TapsDir)
	if err != nil {
		return 0, fmt.Errorf("invalid taps dir: %w", err)
	}

	fmt.Printf("==> Updating taps...\n")
	fetch := exec.Command("git", "fetch", "--depth", "1", "--", "origin", "+refs/heads/main:refs/remotes/origin/main")
	fetch.Dir = tapsDir
	fetch.Stdout = os.Stdout
	fetch.Stderr = os.Stderr
	if err := fetch.Run(); err != nil {
		return 0, fmt.Errorf("update taps: %w", err)
	}
	reset := exec.Command("git", "reset", "--hard", "origin/main")
	reset.Dir = tapsDir
	reset.Stdout = os.Stdout
	reset.Stderr = os.Stderr
	if err := reset.Run(); err != nil {
		return 0, fmt.Errorf("update taps: %w", err)
	}

	// Verify commit signature if configured.
	if err := CheckAfterUpdate(tapsDir, TapVerifyMode()); err != nil {
		return 0, err
	}

	// Count formulas available after update.
	count := 0
	for _, sub := range []string{"core", "cask"} {
		dir := filepath.Join(tapsDir, sub)
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
