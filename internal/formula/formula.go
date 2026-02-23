package formula

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/validation"
	"gopkg.in/yaml.v3"
)

type Formula struct {
	Name         string            `yaml:"name"`
	Version      string            `yaml:"version"`
	Description  string            `yaml:"description"`
	Homepage     string            `yaml:"homepage"`
	License      string            `yaml:"license"`
	URL          map[string]string `yaml:"url"`
	SHA256       map[string]string `yaml:"sha256"`
	Install      InstallSpec       `yaml:"install"`
	Dependencies []string          `yaml:"dependencies"`
	KegOnly      bool              `yaml:"keg_only"`
}

type InstallSpec struct {
	Type            string `yaml:"type"` // "binary" or "archive"
	BinaryName      string `yaml:"binary_name"`
	StripComponents int    `yaml:"strip_components"`
}

func PlatformKey() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}

func (f *Formula) GetURL() (string, error) {
	key := PlatformKey()
	u, ok := f.URL[key]
	if !ok {
		return "", fmt.Errorf("formula %q does not support platform %s; available: %s",
			f.Name, key, sortedMapKeys(f.URL))
	}
	if !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, u)
	}
	return u, nil
}

func (f *Formula) GetSHA256() (string, error) {
	key := PlatformKey()
	s, ok := f.SHA256[key]
	if !ok {
		return "", fmt.Errorf("formula %q has no SHA256 for platform %s", f.Name, key)
	}
	if err := validation.ValidateSHA256(s); err != nil {
		return "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, err)
	}
	return s, nil
}

func (f *Formula) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("formula missing required field: name")
	}
	if !validation.IsValidName(f.Name) {
		return fmt.Errorf("formula name %q contains invalid characters", f.Name)
	}
	if f.Version == "" {
		return fmt.Errorf("formula %q missing required field: version", f.Name)
	}
	if !validation.IsValidVersion(f.Version) {
		return fmt.Errorf("formula %q: version %q contains invalid characters", f.Name, f.Version)
	}
	if len(f.URL) == 0 {
		return fmt.Errorf("formula %q missing required field: url", f.Name)
	}
	for platform, u := range f.URL {
		if !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("formula %q: URL for %s must use HTTPS: %s", f.Name, platform, u)
		}
	}
	if f.Install.Type == "" {
		return fmt.Errorf("formula %q missing required field: install.type", f.Name)
	}
	if f.Install.Type != "binary" && f.Install.Type != "archive" {
		return fmt.Errorf("formula %q has invalid install type %q (must be binary or archive)", f.Name, f.Install.Type)
	}
	for _, dep := range f.Dependencies {
		if !validation.IsValidName(dep) {
			return fmt.Errorf("formula %q: dependency %q contains invalid characters", f.Name, dep)
		}
	}
	return nil
}

func Parse(data []byte) (*Formula, error) {
	var f Formula
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse formula YAML: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// sortedMapKeys returns a deterministic, sorted, comma-separated list of map keys.
func sortedMapKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
