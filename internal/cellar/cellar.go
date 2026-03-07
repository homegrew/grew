package cellar

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/internal/validation"
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
	info, err := os.Stat(kegDir)
	return err == nil && info.IsDir()
}

func (c *Cellar) InstalledVersion(name string) (string, error) {
	if !validation.IsValidName(name) {
		return "", fmt.Errorf("invalid formula name: %q", name)
	}
	kegDir := filepath.Join(c.Path, name)
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

func (c *Cellar) KegPath(name, version string) string {
	// Not validating here since it's just path construction,
	// but maybe good to return error? No, it's a string return.
	// Assume caller validates or it's safe.
	return filepath.Join(c.Path, name, version)
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
		ver, err := c.InstalledVersion(e.Name())
		if err != nil {
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
