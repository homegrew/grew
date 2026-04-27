package cellar

import (
	"fmt"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/validation"
	"os"
	"path/filepath"
	"sort"
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

	// Extra safety: ensure the keg path is within the cellar root before
	// performing any filesystem operations, especially recursive deletion.
	if !c.isUnderCellar(kegPath) {
		return fmt.Errorf("keg path %q escapes cellar root %q", kegPath, c.Path)
	}

	if err := os.MkdirAll(filepath.Dir(kegPath), 0755); err != nil {
		return fmt.Errorf("create cellar dir: %w", err)
	}
	// Remove existing keg if present (reinstall)
	if err := os.RemoveAll(kegPath); err != nil {
		return fmt.Errorf("remove existing keg: %w", err)
	}

	if err := fsutil.CopyTree(stagingDir, kegPath); err != nil {
		if rmErr := os.RemoveAll(kegPath); rmErr != nil {
			return fmt.Errorf("install to cellar: %w; additionally failed to cleanup partial keg: %v", err, rmErr)
		}
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

func (c *Cellar) UninstallVersion(name, version string) error {
	p, err := c.KegPath(name, version)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return nil // already gone
	}
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	// If the formula directory is now empty, remove it too.
	d, _ := c.kegDir(name)
	if entries, err := os.ReadDir(d); err == nil && len(entries) == 0 {
		_ = os.Remove(d)
	}
	return nil
}

func (c *Cellar) IsInstalled(name string) bool {
	versions, err := c.InstalledVersions(name)
	return err == nil && len(versions) > 0
}

func (c *Cellar) InstalledVersion(name string) (string, error) {
	// Reuse InstalledVersions to ensure deterministic ordering and behavior.
	versions, err := c.InstalledVersions(name)
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("formula %q has no installed version", name)
	}
	// Return the latest (highest) installed version.
	return versions[len(versions)-1], nil
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

	// Lexical check first to ensure the path is under the cellar.
	// We use safepath.CheckSubpath which handles absolute path normalization.
	if err := safepath.CheckSubpath(c.Path, p); err != nil {
		return "", fmt.Errorf("path %q escapes cellar %q", p, c.Path)
	}

	// Try to resolve symlinks for the final path.
	resolvedP, err := filepath.EvalSymlinks(p)
	if err != nil {
		// If the path doesn't exist yet, we return the cleaned lexical path.
		// This avoids failures on macOS where /var is a symlink to /private/var
		// but only resolves when the target directory exists.
		return filepath.Clean(p), nil
	}

	// If the path exists and was resolved, we must ensure it still resides
	// within the (also resolved) cellar root.
	resolvedCellar, err := filepath.EvalSymlinks(c.Path)
	if err != nil {
		resolvedCellar, _ = filepath.Abs(c.Path)
	}

	if err := safepath.CheckSubpath(resolvedCellar, resolvedP); err != nil {
		return "", fmt.Errorf("resolved path %q escapes cellar %q", resolvedP, resolvedCellar)
	}
	return resolvedP, nil
}

// isUnderCellar reports whether the given path is located within the Cellar.Path
// directory. It mirrors the logic used by config.Paths.IsUnderRoot and is used
// as a safety check before performing destructive operations such as
// recursive deletion.
func (c *Cellar) isUnderCellar(path string) bool {
	if c.Path == "" || path == "" {
		return false
	}
	return safepath.IsSubpath(c.Path, path)
}

// kegDir returns the validated path to a formula's directory in the cellar.
func (c *Cellar) kegDir(name string) (string, error) {
	if !validation.IsValidName(name) {
		return "", fmt.Errorf("invalid formula name: %q", name)
	}
	d := filepath.Join(c.Path, name)
	if err := safepath.CheckSubpath(c.Path, d); err != nil {
		return "", fmt.Errorf("path %q escapes cellar %q", d, c.Path)
	}
	return d, nil
}

func (c *Cellar) List() ([]InstalledPackage, error) {
	absCellar, err := filepath.Abs(c.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve cellar: %w", err)
	}
	entries, err := os.ReadDir(absCellar)
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
	return os.WriteFile(filepath.Join(d, "PINNED"), []byte{}, 0644)
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
	if !c.IsInstalled(name) {
		return false
	}
	d, _ := c.kegDir(name)
	_, err := os.Stat(filepath.Join(d, "PINNED"))
	return err == nil
}
