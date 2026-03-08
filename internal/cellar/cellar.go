package cellar

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/pkg/validation"
)

type InstalledPackage struct {
	Name    string
	Version string
	Path    string
}

type Cellar struct {
	Path string
}

func (c *Cellar) Install(name, version, stagingDir string) error {
	if !validation.IsValidName(name) || !validation.IsValidVersion(version) {
		return fmt.Errorf("invalid name or version")
	}
	kegPath := filepath.Join(c.Path, name, version)

	// Verify the constructed keg path resolves within the cellar.
	if err := c.ensureWithinCellar(kegPath); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(kegPath), 0755); err != nil {
		return fmt.Errorf("create cellar dir: %w", err)
	}
	// Remove existing keg if present (reinstall)
	os.RemoveAll(kegPath)

	if err := fsutil.CopyTree(stagingDir, kegPath); err != nil {
		os.RemoveAll(kegPath)
		return fmt.Errorf("install to cellar: %w", err)
	}
	return nil
}

func (c *Cellar) Uninstall(name string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid formula name: %q", name)
	}
	kegDir := filepath.Join(c.Path, name)
	if err := c.ensureWithinCellar(kegDir); err != nil {
		return err
	}
	if _, err := os.Stat(kegDir); os.IsNotExist(err) {
		return fmt.Errorf("formula %q is not installed", name)
	}
	return os.RemoveAll(kegDir)
}

func (c *Cellar) IsInstalled(name string) bool {
	if !validation.IsValidName(name) {
		return false
	}
	kegDir := filepath.Join(c.Path, name)
	if err := c.ensureWithinCellar(kegDir); err != nil {
		return false
	}
	info, err := os.Stat(kegDir)
	return err == nil && info.IsDir()
}

func (c *Cellar) InstalledVersion(name string) (string, error) {
	if !validation.IsValidName(name) {
		return "", fmt.Errorf("invalid formula name: %q", name)
	}
	kegDir := filepath.Join(c.Path, name)
	if err := c.ensureWithinCellar(kegDir); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(kegDir)
	if err != nil {
		return "", fmt.Errorf("formula %q is not installed", name)
	}
	for _, e := range entries {
		if e.IsDir() {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("formula %q has no installed version", name)
}

// InstalledVersions returns all version directories for a formula, sorted ascending.
func (c *Cellar) InstalledVersions(name string) ([]string, error) {
	if !validation.IsValidName(name) {
		return nil, fmt.Errorf("invalid formula name: %q", name)
	}
	kegDir := filepath.Join(c.Path, name)
	if err := c.ensureWithinCellar(kegDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(kegDir)
	if err != nil {
		return nil, fmt.Errorf("formula %q is not installed", name)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)
	return versions, nil
}

// KegPath returns the path to a keg directory. The name and version are
// validated to prevent path traversal. Returns an empty string if invalid.
func (c *Cellar) KegPath(name, version string) string {
	if !validation.IsValidName(name) || !validation.IsValidVersion(version) {
		return ""
	}
	p := filepath.Join(c.Path, name, version)
	if err := c.ensureWithinCellar(p); err != nil {
		return ""
	}
	return p
}

func (c *Cellar) List() ([]InstalledPackage, error) {
	entries, err := os.ReadDir(c.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cellar: %w", err)
	}

	var packages []InstalledPackage
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Validate the directory name to prevent path traversal if the
		// cellar contains unexpected entries.
		if !validation.IsValidName(e.Name()) {
			continue
		}
		ver, err := c.InstalledVersion(e.Name())
		if err != nil {
			continue
		}
		if !validation.IsValidVersion(ver) {
			continue
		}
		packages = append(packages, InstalledPackage{
			Name:    e.Name(),
			Version: ver,
			Path:    filepath.Join(c.Path, e.Name(), ver),
		})
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})
	return packages, nil
}

// Pin marks a formula as pinned, preventing it from being upgraded.
func (c *Cellar) Pin(name string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid formula name: %q", name)
	}
	kegDir := filepath.Join(c.Path, name)
	if err := c.ensureWithinCellar(kegDir); err != nil {
		return err
	}
	if _, err := os.Stat(kegDir); os.IsNotExist(err) {
		return fmt.Errorf("formula %q is not installed", name)
	}
	return os.WriteFile(filepath.Join(kegDir, "PINNED"), nil, 0644)
}

// Unpin removes the pin from a formula, allowing it to be upgraded.
func (c *Cellar) Unpin(name string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid formula name: %q", name)
	}
	kegDir := filepath.Join(c.Path, name)
	if err := c.ensureWithinCellar(kegDir); err != nil {
		return err
	}
	pinFile := filepath.Join(kegDir, "PINNED")
	if err := os.Remove(pinFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsPinned returns true if a formula is pinned.
func (c *Cellar) IsPinned(name string) bool {
	if !validation.IsValidName(name) {
		return false
	}
	pinFile := filepath.Join(c.Path, name, "PINNED")
	_, err := os.Stat(pinFile)
	return err == nil
}

// ensureWithinCellar verifies that a resolved path stays within the cellar
// directory. This prevents path traversal via crafted names or symlinks.
func (c *Cellar) ensureWithinCellar(target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	absCellar, err := filepath.Abs(c.Path)
	if err != nil {
		return fmt.Errorf("resolve cellar: %w", err)
	}
	if absTarget != absCellar && !strings.HasPrefix(absTarget, absCellar+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes cellar %q", target, c.Path)
	}
	return nil
}
