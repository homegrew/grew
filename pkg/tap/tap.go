package tap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
)

const defaultRepoURL = "https://github.com/homegrew/homegrew-taps.git"

type Manager struct {
	TapsDir string
}

// cleanDir validates and cleans a directory path to prevent argument
// injection. The result is always an absolute, Clean'd path.
func cleanDir(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("empty path")
	}
	// Reject traversal before resolving to absolute (go/path-injection).
	if _, err := safepath.CleanPath(dir); err != nil {
		return "", fmt.Errorf("invalid directory path: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	clean := filepath.Clean(abs)
	if strings.HasPrefix(filepath.Base(clean), "-") {
		return "", fmt.Errorf("path component starts with dash: %q", dir)
	}
	return clean, nil
}

// CoreTapPath returns the path to the official core tap.
func (m *Manager) CoreTapPath() string {
	return filepath.Join(m.TapsDir, "homegrew", "homegrew-taps")
}

// EnsureCloned clones the official core tap if it hasn't been cloned yet.
func (m *Manager) EnsureCloned() error {
	if m.TapsDir == "" {
		return fmt.Errorf("invalid taps dir: empty path")
	}

	corePath := m.CoreTapPath()
	absDir, err := filepath.Abs(corePath)
	if err != nil {
		return fmt.Errorf("invalid core tap dir: %w", err)
	}
	corePath = filepath.Clean(absDir)

	gitDir := filepath.Join(corePath, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil // already cloned
	}

	// If corePath exists but isn't a git repo (e.g. leftover from a previous install),
	// remove it so the clone can succeed.
	if entries, err := os.ReadDir(corePath); err == nil && len(entries) > 0 {
		if err := os.RemoveAll(corePath); err != nil {
			return fmt.Errorf("clear stale core tap dir: %w", err)
		}
	}

	// Ensure parent directory exists (Taps/homegrew)
	if err := os.MkdirAll(filepath.Dir(corePath), 0755); err != nil {
		return fmt.Errorf("create core tap parent dir: %w", err)
	}

	ui.FprintArrow(os.Stdout, "Cloning core taps from %s", defaultRepoURL)
	cmd := exec.Command("git", "clone", "--depth", "1", "--", defaultRepoURL, corePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clone core taps repo: %w", err)
	}

	// Verify commit signature if configured.
	if err := CheckAfterUpdate(corePath, TapVerifyMode()); err != nil {
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

// Update pulls the latest definitions for ALL installed taps.
// Returns the number of taps updated and the total number of packages (formulas and casks) found.
func (m *Manager) Update() (int, int, error) {
	if err := m.EnsureCloned(); err != nil {
		return 0, 0, err
	}

	users, err := os.ReadDir(m.TapsDir)
	if err != nil {
		return 0, 0, fmt.Errorf("read taps directory: %w", err)
	}

	totalCount := 0
	tapsCount := 0
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(m.TapsDir, user.Name()))
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			repoPath := filepath.Join(m.TapsDir, user.Name(), repo.Name())
			if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
				continue
			}

			ui.FprintArrow(os.Stdout, "Updating tap %s/%s...", user.Name(), repo.Name())
			fetch := exec.Command("git", "fetch", "--depth", "1", "--", "origin")
			fetch.Dir = repoPath
			if err := fetch.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "%s failed to fetch tap %s/%s: %v\n", ui.ArrowWarning(os.Stderr), user.Name(), repo.Name(), err)
				continue
			}
			// Determine default branch (usually main or master)
			revParse := exec.Command("git", "rev-parse", "--abbrev-ref", "origin/HEAD")
			revParse.Dir = repoPath
			out, err := revParse.Output()
			branch := "origin/main"
			if err == nil {
				branch = strings.TrimSpace(string(out))
			}

			reset := exec.Command("git", "reset", "--hard", branch)
			reset.Dir = repoPath
			if err := reset.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "%s failed to reset tap %s/%s: %v\n", ui.ArrowWarning(os.Stderr), user.Name(), repo.Name(), err)
				continue
			}

			if err := CheckAfterUpdate(repoPath, TapVerifyMode()); err != nil {
				fmt.Fprintf(os.Stderr, "%s verification failed for tap %s/%s: %v\n", ui.ArrowWarning(os.Stderr), user.Name(), repo.Name(), err)
				continue
			}

			tapsCount++

			// Count formulas and casks in this tap (recursively)
			for _, sub := range []string{"", "core", "Formula", "cask", "Casks"} {
				dir := filepath.Join(repoPath, sub)
				countPackagesRecursive(dir, &totalCount)
			}
		}
	}
	return tapsCount, totalCount, nil
}

func (m *Manager) safeTapRepoPath(user, repo string) (string, error) {
	if user == "" || repo == "" {
		return "", errors.New("tap path components must be non-empty")
	}
	if strings.Contains(user, string(os.PathSeparator)) || strings.Contains(repo, string(os.PathSeparator)) {
		return "", errors.New("tap path components must not contain path separators")
	}
	if user == "." || user == ".." || repo == "." || repo == ".." {
		return "", errors.New("tap path components must not be dot segments")
	}

	baseAbs, err := filepath.Abs(m.TapsDir)
	if err != nil {
		return "", fmt.Errorf("resolve taps dir: %w", err)
	}
	baseAbs = filepath.Clean(baseAbs)

	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, user, repo))
	if err != nil {
		return "", fmt.Errorf("resolve tap path: %w", err)
	}
	targetAbs = filepath.Clean(targetAbs)

	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", fmt.Errorf("verify tap path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("refusing path outside taps dir: %q", targetAbs)
	}

	return targetAbs, nil
}

// Add clones a new tap. name should be in "user/repo" format.
func (m *Manager) Add(name, customURL string) error {
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid tap name %q; expected \"user/repo\"", name)
	}
	user, repo := parts[0], parts[1]

	url := customURL
	if url == "" {
		url = fmt.Sprintf("https://github.com/%s/homegrew-%s.git", user, repo)
	}

	repoPath, err := m.safeTapRepoPath(user, repo)
	if err != nil {
		return fmt.Errorf("invalid tap path: %w", err)
	}
	if _, err := os.Stat(repoPath); err == nil {
		return fmt.Errorf("tap %s is already installed", name)
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0755); err != nil {
		return fmt.Errorf("create tap parent dir: %w", err)
	}

	ui.FprintArrow(os.Stdout, "Tapping %s from %s", name, url)
	cmd := exec.Command("git", "clone", "--depth", "1", "--", url, repoPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clone tap: %w", err)
	}

	if err := CheckAfterUpdate(repoPath, TapVerifyMode()); err != nil {
		return fmt.Errorf("verify tap: %w", err)
	}

	return nil
}

// Remove deletes an installed tap.
func (m *Manager) Remove(name string) error {
	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid tap name %q; expected \"user/repo\"", name)
	}
	user, repo := parts[0], parts[1]

	repoPath, err := m.safeTapRepoPath(user, repo)
	if err != nil {
		return fmt.Errorf("invalid tap path: %w", err)
	}
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return fmt.Errorf("tap %s is not installed", name)
	}

	ui.FprintArrow(os.Stdout, "Untapping %s...", name)
	return os.RemoveAll(repoPath)
}

// List returns all installed taps in "user/repo" format.
func (m *Manager) List() ([]string, error) {
	users, err := os.ReadDir(m.TapsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var taps []string
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(m.TapsDir, user.Name()))
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			repoPath := filepath.Join(m.TapsDir, user.Name(), repo.Name())
			if _, err := os.Stat(filepath.Join(repoPath, ".git")); err == nil {
				taps = append(taps, fmt.Sprintf("%s/%s", user.Name(), repo.Name()))
			}
		}
	}
	return taps, nil
}

// countPackagesRecursive counts YAML/YML files recursively in a directory.
func countPackagesRecursive(dir string, count *int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			name := e.Name()
			if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
				*count++
			}
		} else {
			countPackagesRecursive(filepath.Join(dir, e.Name()), count)
		}
	}
}
