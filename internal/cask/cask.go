package cask

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/validation"

	"gopkg.in/yaml.v3"
)

type SourceSpec struct {
	URL    string `yaml:"url,omitempty"`
	SHA256 string `yaml:"sha256,omitempty"`
	SHA512 string `yaml:"sha512,omitempty"`
}

// Cask represents a macOS application package definition.
type Cask struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description,omitempty"`
	Homepage    string            `yaml:"homepage,omitempty"`
	License     string            `yaml:"license,omitempty"`
	Caveats     string            `yaml:"caveats,omitempty"`
	URL         map[string]string `yaml:"url,omitempty"`
	SHA256      map[string]string `yaml:"sha256,omitempty"`
	SHA512      map[string]string `yaml:"sha512,omitempty"`
	Artifacts   Artifacts         `yaml:"artifacts,omitempty"`
	Source      SourceSpec        `yaml:"source,omitempty"`

	// Local fields (not in YAML)
	Tap string `yaml:"-"`
}

// Artifacts describes what to install from the downloaded archive.
type Artifacts struct {
	App []string `yaml:"app,omitempty"` // .app bundles to copy to ~/Applications
	Pkg []string `yaml:"pkg,omitempty"` // .pkg installers to run (not implemented yet)
	Bin []string `yaml:"bin,omitempty"` // binaries to symlink into grew bin/
}

func PlatformKey() string {
	return GetPlatformKey(runtime.GOOS, runtime.GOARCH)
}

func GetPlatformKey(osName, arch string) string {
	return osName + "_" + arch
}

func (c *Cask) GetURL() (string, error) {
	return c.GetURLForPlatform(runtime.GOOS, runtime.GOARCH)
}

func (c *Cask) GetURLForPlatform(osName, arch string) (string, error) {
	key := GetPlatformKey(osName, arch)
	u, ok := c.URL[key]
	if !ok {
		return "", fmt.Errorf("cask %q does not support platform %s; available: %s",
			c.Name, key, sortedKeys(c.URL))
	}
	if !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("cask %q: refusing to download over insecure HTTP: %s", c.Name, u)
	}
	return u, nil
}

func (c *Cask) GetSHA256() (string, error) {
	return c.GetSHA256ForPlatform(runtime.GOOS, runtime.GOARCH)
}

func (c *Cask) GetSHA256ForPlatform(osName, arch string) (string, error) {
	key := GetPlatformKey(osName, arch)
	s, ok := c.SHA256[key]
	if !ok {
		return "", fmt.Errorf("cask %q has no SHA256 for platform %s", c.Name, key)
	}
	if err := validation.ValidateSHA256(s); err != nil {
		return "", fmt.Errorf("cask %q: invalid SHA256 for %s: %w", c.Name, key, err)
	}
	return s, nil
}

func (c *Cask) GetSourceURL() (string, error) {
	if c.Source.URL == "" {
		return "", fmt.Errorf("cask %q has no source_url defined", c.Name)
	}
	if !strings.HasPrefix(c.Source.URL, "https://") {
		return "", fmt.Errorf("cask %q: refusing to download over insecure HTTP: %s", c.Name, c.Source.URL)
	}
	return c.Source.URL, nil
}

func (c *Cask) GetSourceSHA256() (string, error) {
	if c.Source.SHA256 == "" {
		return "", fmt.Errorf("cask %q has no source_sha256 defined", c.Name)
	}
	if err := validation.ValidateSHA256(c.Source.SHA256); err != nil {
		return "", fmt.Errorf("cask %q: invalid source_sha256: %w", c.Name, err)
	}
	return c.Source.SHA256, nil
}

// GetSHA512 returns the SHA512 checksum for the current platform.
func (c *Cask) GetSHA512() string {
	key := PlatformKey()
	return c.SHA512[key]
}

func (c *Cask) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("cask missing required field: name")
	}
	if !validation.IsValidName(c.Name) {
		return fmt.Errorf("cask name %q contains invalid characters", c.Name)
	}
	if c.Version == "" {
		return fmt.Errorf("cask %q missing required field: version", c.Name)
	}
	if !validation.IsValidVersion(c.Version) {
		return fmt.Errorf("cask %q: version %q contains invalid characters", c.Name, c.Version)
	}
	if len(c.URL) == 0 {
		return fmt.Errorf("cask %q missing required field: url", c.Name)
	}
	for platform, u := range c.URL {
		if !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("cask %q: URL for %s must use HTTPS: %s", c.Name, platform, u)
		}
	}
	for platform, hash := range c.SHA256 {
		if err := validation.ValidateSHA256(hash); err != nil {
			return fmt.Errorf("cask %q: invalid SHA256 for %s: %w", c.Name, platform, err)
		}
	}
	for platform, hash := range c.SHA512 {
		if err := validation.ValidateSHA512(hash); err != nil {
			return fmt.Errorf("cask %q: invalid SHA512 for %s: %w", c.Name, platform, err)
		}
	}
	if len(c.Artifacts.App) == 0 && len(c.Artifacts.Pkg) == 0 && len(c.Artifacts.Bin) == 0 {
		return fmt.Errorf("cask %q: must declare at least one artifact (yaml keys: app, pkg, or bin)", c.Name)
	}
	for _, app := range c.Artifacts.App {
		if !strings.HasSuffix(app, ".app") {
			return fmt.Errorf("cask %q: app artifact %q must end with .app", c.Name, app)
		}
	}
	return nil
}

func Parse(data []byte) (*Cask, error) {
	var c Cask
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse cask YAML: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Loader reads cask definitions from tap directories.
type Loader struct {
	TapDir   string
	DebugLog func(format string, args ...any)
}

func (l *Loader) debugf(format string, args ...any) {
	if l.DebugLog != nil {
		l.DebugLog(format, args...)
	}
}

func (l *Loader) LoadByName(name string) (*Cask, error) {
	name = strings.TrimSuffix(name, ".yaml")

	// Handle tap-qualified names (e.g., "user/repo/name" or "cask/name")
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		caskName := parts[len(parts)-1]
		tapPath := parts[:len(parts)-1]

		// Validate components
		if err := safepath.SafePathComponent(caskName + ".yaml"); err != nil {
			return nil, fmt.Errorf("invalid cask name: %q", caskName)
		}
		for _, p := range tapPath {
			if err := safepath.SafePathComponent(p); err != nil {
				return nil, fmt.Errorf("invalid tap name component: %q", p)
			}
		}

		// Support both homegrew/homegrew-taps and legacy core/cask references
		var paths []string
		if len(tapPath) == 1 {
			if tapPath[0] == "core" || tapPath[0] == "cask" {
				// Redirect cask/foo to homegrew/homegrew-taps/cask/foo
				paths = append(paths, filepath.Join(l.TapDir, "homegrew", "homegrew-taps", tapPath[0], caskName+".yaml"))
				paths = append(paths, filepath.Join(l.TapDir, "homegrew", "homegrew-taps", "Casks", caskName+".yaml"))
			}
		}
		
		// Standard layouts
		paths = append(paths, filepath.Join(append([]string{l.TapDir}, append(tapPath, caskName+".yaml")...)...))
		paths = append(paths, filepath.Join(append([]string{l.TapDir}, append(tapPath, "cask", caskName+".yaml")...)...))
		paths = append(paths, filepath.Join(append([]string{l.TapDir}, append(tapPath, "Casks", caskName+".yaml")...)...))

		for _, path := range paths {
			c, err := l.loadFromFileWithPath(path)
			if err == nil {
				return c, nil
			}
		}
		return nil, fmt.Errorf("cask not found in tap %q: %q", strings.Join(tapPath, "/"), caskName)
	}

	if err := safepath.SafePathComponent(name + ".yaml"); err != nil {
		return nil, fmt.Errorf("invalid cask name: %q", name)
	}

	users, err := os.ReadDir(l.TapDir)
	if err != nil {
		return nil, fmt.Errorf("read taps directory: %w", err)
	}

	var lastErr error
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(user.Name()); err != nil {
			continue
		}

		repos, err := os.ReadDir(filepath.Join(l.TapDir, user.Name()))
		if err != nil {
			continue
		}

		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			if err := safepath.SafePathComponent(repo.Name()); err != nil {
				continue
			}

			// Try tap root, tap/cask/, and tap/Casks/ subdirectories.
			paths := []string{
				filepath.Join(l.TapDir, user.Name(), repo.Name(), name+".yaml"),
				filepath.Join(l.TapDir, user.Name(), repo.Name(), "cask", name+".yaml"),
				filepath.Join(l.TapDir, user.Name(), repo.Name(), "Casks", name+".yaml"),
			}

			for _, path := range paths {
				c, err := l.loadFromFileWithPath(path)
				if err == nil {
					return c, nil
				}
				if !os.IsNotExist(err) {
					lastErr = fmt.Errorf("failed to parse %s: %w", path, err)
				}
			}
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("cask not found: %q (%v)", name, lastErr)
	}
	return nil, fmt.Errorf("cask not found: %q", name)
}

func (l *Loader) LoadAll() ([]*Cask, error) {
	users, err := os.ReadDir(l.TapDir)
	if err != nil {
		return nil, fmt.Errorf("read taps directory: %w", err)
	}

	var casks []*Cask
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(user.Name()); err != nil {
			continue
		}

		repos, err := os.ReadDir(filepath.Join(l.TapDir, user.Name()))
		if err != nil {
			continue
		}

		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			if err := safepath.SafePathComponent(repo.Name()); err != nil {
				continue
			}

			// Check tap root, tap/cask/, and tap/Casks/ subdirectories.
			subdirs := []string{"", "cask", "Casks"}
			repoPath := filepath.Join(l.TapDir, user.Name(), repo.Name())
			for _, subdir := range subdirs {
				caskDir := filepath.Join(repoPath, subdir)
				entries, err := os.ReadDir(caskDir)
				if err != nil {
					continue
				}
				for _, e := range entries {
					if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
						continue
					}
					if err := safepath.SafePathComponent(e.Name()); err != nil {
						continue
					}
					c, err := l.loadFromFileWithPath(filepath.Join(caskDir, e.Name()))
					if err != nil {
						continue
					}
					casks = append(casks, c)
				}
			}
		}
	}
	return casks, nil
}

func (l *Loader) loadFromFile(filename string) (*Cask, error) {
	return l.loadFromFileWithPath(filepath.Join(l.TapDir, "cask", filename))
}

func (l *Loader) loadFromFileWithPath(path string) (*Cask, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absPath = filepath.Clean(absPath)
	if err := safepath.SafeAbsolutePath(absPath); err != nil {
		return nil, fmt.Errorf("invalid cask path %q: %w", absPath, err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, err
	}

	// Infer tap name from path (e.g. Taps/user/repo/...)
	rel, err := filepath.Rel(l.TapDir, absPath)
	if err == nil {
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 2 {
			c.Tap = parts[0] + "/" + parts[1]
		}
	}

	return c, nil
}

// Caskroom manages installed cask metadata.
type Caskroom struct {
	Path string // ~/.homegrew/Caskroom
}

func (cr *Caskroom) IsInstalled(name string) bool {
	if !validation.IsValidName(name) {
		return false
	}
	if err := safepath.SafeAbsolutePath(cr.Path); err != nil {
		return false
	}
	path, err := safepath.SafeJoin(cr.Path, name)
	if err != nil {
		return false
	}
	if err := safepath.CheckSubpath(cr.Path, path); err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (cr *Caskroom) InstalledVersion(name string) (string, error) {
	if !validation.IsValidName(name) {
		return "", fmt.Errorf("invalid cask name: %q", name)
	}
	if err := safepath.SafeAbsolutePath(cr.Path); err != nil {
		return "", fmt.Errorf("invalid caskroom path: %w", err)
	}
	path, err := safepath.SafeJoin(cr.Path, name)
	if err != nil {
		return "", err
	}
	return readInstalledVersion(path, name)
}

func readInstalledVersion(path, name string) (string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("cask %q is not installed", name)
	}
	for _, e := range entries {
		if e.IsDir() && validation.IsValidVersion(e.Name()) {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("cask %q has no installed version", name)
}

// Record marks a cask as installed by creating Caskroom/<name>/<version>/.
func (cr *Caskroom) Record(name, version string) error {
	if !validation.IsValidName(name) || !validation.IsValidVersion(version) {
		return fmt.Errorf("invalid name or version")
	}
	if err := safepath.SafeAbsolutePath(cr.Path); err != nil {
		return fmt.Errorf("invalid caskroom path: %w", err)
	}
	dir, err := safepath.SafeJoin(cr.Path, name, version)
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

// Remove deletes a cask's caskroom entry.
func (cr *Caskroom) Remove(name string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid cask name: %q", name)
	}
	if err := safepath.SafeAbsolutePath(cr.Path); err != nil {
		return fmt.Errorf("invalid caskroom path: %w", err)
	}
	dir, err := safepath.SafeJoin(cr.Path, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("cask %q is not installed", name)
	}
	return os.RemoveAll(dir)
}

type InstalledCask struct {
	Name    string
	Version string
}

func (cr *Caskroom) List() ([]InstalledCask, error) {
	if err := safepath.SafeAbsolutePath(cr.Path); err != nil {
		return nil, err
	}

	path, err := filepath.Abs(cr.Path)
	if err != nil {
		return nil, err
	}
	path = filepath.Clean(path)

	// Canonicalize when possible to avoid symlink-based redirection surprises.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if err := safepath.SafeAbsolutePath(path); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var casks []InstalledCask
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !validation.IsValidName(e.Name()) {
			continue
		}
		caskPath, err := safepath.SafeJoin(path, e.Name())
		if err != nil {
			continue
		}
		ver, err := readInstalledVersion(caskPath, e.Name())
		if err != nil {
			continue
		}
		casks = append(casks, InstalledCask{Name: e.Name(), Version: ver})
	}
	sort.Slice(casks, func(i, j int) bool {
		return casks[i].Name < casks[j].Name
	})
	return casks, nil
}

func sortedKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
