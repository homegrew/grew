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
	kegPath, err := c.KegPath(name, version)
	if err != nil {
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
	d, err := c.kegDir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(d); os.IsNotExist(err) {
		return fmt.Errorf("formula %q is not installed", name)
	}
	return os.RemoveAll(d)
}

func (c *Cellar) IsInstalled(name string) bool {
	d, err := c.kegDir(name)
	if err != nil {
		return false
	}
	info, err := os.Stat(d)
	return err == nil && info.IsDir()
}

func (c *Cellar) InstalledVersion(name string) (string, error) {
	d, err := c.kegDir(name)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return "", fmt.Errorf("formula %q is not installed", name)
	}
	for _, e := range entries {
		if e.IsDir() && validation.IsValidVersion(e.Name()) {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("formula %q has no installed version", name)
}

// InstalledVersions returns all version directories for a formula, sorted ascending.
func (c *Cellar) InstalledVersions(name string) ([]string, error) {
	d, err := c.kegDir(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return nil, fmt.Errorf("formula %q is not installed", name)
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && validation.IsValidVersion(e.Name()) {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)
	return versions, nil
}

// KegPath returns the path to a keg directory. The name and version are
// validated and the result is verified to stay within the cellar.
func (c *Cellar) KegPath(name, version string) (string, error) {
	if !validation.IsValidName(name) || !validation.IsValidVersion(version) {
		return "", fmt.Errorf("invalid name or version")
	}
	p := filepath.Join(c.Path, name, version)
	absP, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	absCellar, err := filepath.Abs(c.Path)
	if err != nil {
		return "", fmt.Errorf("resolve cellar: %w", err)
	}
	rel, err := filepath.Rel(absCellar, absP)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes cellar %q", p, c.Path)
	}
	return p, nil
}

// kegDir returns the validated path to a formula's directory in the cellar.
func (c *Cellar) kegDir(name string) (string, error) {
	if !validation.IsValidName(name) {
		return "", fmt.Errorf("invalid formula name: %q", name)
	}
	d := filepath.Join(c.Path, name)
	absD, err := filepath.Abs(d)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	absCellar, err := filepath.Abs(c.Path)
	if err != nil {
		return "", fmt.Errorf("resolve cellar: %w", err)
	}
	rel, err := filepath.Rel(absCellar, absD)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes cellar %q", d, c.Path)
	}
	return d, nil
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
		kegPath, err := c.KegPath(e.Name(), ver)
		if err != nil {
			continue
		}
		packages = append(packages, InstalledPackage{
			Name:    e.Name(),
			Version: ver,
			Path:    kegPath,
		})
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})
	return packages, nil
}

// Pin marks a formula as pinned, preventing it from being upgraded.
func (c *Cellar) Pin(name string) error {
	d, err := c.kegDir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(d); os.IsNotExist(err) {
		return fmt.Errorf("formula %q is not installed", name)
	}
	return os.WriteFile(filepath.Join(d, "PINNED"), nil, 0644)
}

// Unpin removes the pin from a formula, allowing it to be upgraded.
func (c *Cellar) Unpin(name string) error {
	d, err := c.kegDir(name)
	if err != nil {
		return err
	}
	pinFile := filepath.Join(d, "PINNED")
	if err := os.Remove(pinFile); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsPinned returns true if a formula is pinned.
func (c *Cellar) IsPinned(name string) bool {
	d, err := c.kegDir(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(d, "PINNED"))
	return err == nil
}

