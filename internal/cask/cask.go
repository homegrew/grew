package cask

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/validation"

	"gopkg.in/yaml.v3"
)

// Cask represents a macOS application package definition.
type Cask struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Description string            `yaml:"description"`
	Homepage    string            `yaml:"homepage"`
	License     string            `yaml:"license"`
	URL         map[string]string `yaml:"url"`
	SHA256      map[string]string `yaml:"sha256"`
	Artifacts   Artifacts         `yaml:"artifacts"`
}

// Artifacts describes what to install from the downloaded archive.
type Artifacts struct {
	App []string `yaml:"app"`      // .app bundles to copy to ~/Applications
	Pkg []string `yaml:"internal"` // .internal installers to run (not implemented yet)
	Bin []string `yaml:"bin"`      // binaries to symlink into grew bin/
}

func PlatformKey() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}

func (c *Cask) GetURL() (string, error) {
	key := PlatformKey()
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
	key := PlatformKey()
	s, ok := c.SHA256[key]
	if !ok {
		return "", fmt.Errorf("cask %q has no SHA256 for platform %s", c.Name, key)
	}
	if len(s) != 64 {
		return "", fmt.Errorf("cask %q: SHA256 for %s must be 64 hex characters, got %d", c.Name, key, len(s))
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", fmt.Errorf("cask %q: invalid SHA256 hex for %s: %w", c.Name, key, err)
	}
	return s, nil
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
	if len(c.Artifacts.App) == 0 && len(c.Artifacts.Pkg) == 0 && len(c.Artifacts.Bin) == 0 {
		return fmt.Errorf("cask %q: must declare at least one artifact (app, internal, or bin)", c.Name)
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
	// Look in "cask" subdirectory of taps
	caskDir := filepath.Join(l.TapDir, "cask")
	path := filepath.Join(caskDir, name+".yaml")
	return l.loadFromFile(path)
}

func (l *Loader) LoadAll() ([]*Cask, error) {
	caskDir := filepath.Join(l.TapDir, "cask")
	entries, err := os.ReadDir(caskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cask tap: %w", err)
	}
	var casks []*Cask
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		c, err := l.loadFromFile(filepath.Join(caskDir, e.Name()))
		if err != nil {
			l.debugf("failed to parse cask %s: %v\n", e.Name(), err)
			continue
		}
		casks = append(casks, c)
	}
	return casks, nil
}

func (l *Loader) loadFromFile(path string) (*Cask, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Caskroom manages installed cask metadata.
type Caskroom struct {
	Path string // ~/.grew/Caskroom
}

func (cr *Caskroom) IsInstalled(name string) bool {
	if !validation.IsValidName(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(cr.Path, name))
	return err == nil && info.IsDir()
}

func (cr *Caskroom) InstalledVersion(name string) (string, error) {
	if !validation.IsValidName(name) {
		return "", fmt.Errorf("invalid cask name: %q", name)
	}
	entries, err := os.ReadDir(filepath.Join(cr.Path, name))
	if err != nil {
		return "", fmt.Errorf("cask %q is not installed", name)
	}
	for _, e := range entries {
		if e.IsDir() {
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
	dir := filepath.Join(cr.Path, name, version)
	return os.MkdirAll(dir, 0755)
}

// Remove deletes a cask's caskroom entry.
func (cr *Caskroom) Remove(name string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid cask name: %q", name)
	}
	dir := filepath.Join(cr.Path, name)
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
	entries, err := os.ReadDir(cr.Path)
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
		ver, err := cr.InstalledVersion(e.Name())
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
