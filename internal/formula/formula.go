package formula

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/validation"
	"gopkg.in/yaml.v3"
)

type SourceSpec struct {
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256"`
}

type BottleSpec struct {
	URL    string `yaml:"url"`
	SHA256 string `yaml:"sha256"`
}

type BuildSpec struct {
	Configure []string `yaml:"configure"`
	Install   []string `yaml:"install"`
}

type Formula struct {
	Name         string            `yaml:"name"`
	Version      string            `yaml:"version"`
	Description  string            `yaml:"description"`
	Homepage     string            `yaml:"homepage"`
	License      string            `yaml:"license"`
	URL          map[string]string `yaml:"url"`
	SHA256       map[string]string `yaml:"sha256"`
	SourceURL    string            `yaml:"source_url"`
	SourceSHA256 string            `yaml:"source_sha256"`
	Install      InstallSpec       `yaml:"install"`
	PostInstall  string            `yaml:"post_install"`
	Dependencies []string          `yaml:"dependencies"`
	KegOnly      bool              `yaml:"keg_only"`
	// New schema fields
	Bottle            map[string]BottleSpec `yaml:"bottle"`
	Source            SourceSpec            `yaml:"source"`
	BuildDependencies []string              `yaml:"build_dependencies"`
	LinuxDependencies []string              `yaml:"linux_dependencies"`
	Build             BuildSpec             `yaml:"build"`
	Service           *ServiceSpec          `yaml:"service"`
}

type ServiceSpec struct {
	Run          []string `yaml:"run"`
	RunType      string   `yaml:"run_type"`
	WorkingDir   string   `yaml:"working_dir"`
	LogPath      string   `yaml:"log_path"`
	ErrorLogPath string   `yaml:"error_log_path"`
	KeepAlive    bool     `yaml:"keep_alive"`
}

type InstallSpec struct {
	Type            string `yaml:"type"` // "binary" or "archive"
	BinaryName      string `yaml:"binary_name"`
	StripComponents int    `yaml:"strip_components"`
	Format          string `yaml:"format"` // optional: "tar.gz", "zip" — used when URL has no extension
}

func PlatformKey() string {
	return runtime.GOOS + "_" + runtime.GOARCH
}

func (f *Formula) GetURL() (string, error) {
	key := PlatformKey()
	// New format support
	if len(f.Bottle) > 0 {
		if b, ok := f.Bottle[key]; ok {
			if !strings.HasPrefix(b.URL, "https://") {
				return "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, b.URL)
			}
			return b.URL, nil
		}
	}
	// Fallback to old format
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

func (f *Formula) GetSourceURL() (string, error) {
	if f.Source.URL != "" {
		if !strings.HasPrefix(f.Source.URL, "https://") {
			return "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, f.Source.URL)
		}
		return f.Source.URL, nil
	}
	// Fallback
	if f.SourceURL == "" {
		return "", fmt.Errorf("formula %q has no source_url defined", f.Name)
	}
	if !strings.HasPrefix(f.SourceURL, "https://") {
		return "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, f.SourceURL)
	}
	return f.SourceURL, nil
}

func (f *Formula) GetSourceSHA256() (string, error) {
	if f.Source.SHA256 != "" {
		if err := validation.ValidateSHA256(f.Source.SHA256); err != nil {
			return "", fmt.Errorf("formula %q: invalid source_sha256: %w", f.Name, err)
		}
		return f.Source.SHA256, nil
	}
	// Fallback
	if f.SourceSHA256 == "" {
		return "", fmt.Errorf("formula %q has no source_sha256 defined", f.Name)
	}
	if err := validation.ValidateSHA256(f.SourceSHA256); err != nil {
		return "", fmt.Errorf("formula %q: invalid source_sha256: %w", f.Name, err)
	}
	return f.SourceSHA256, nil
}

func (f *Formula) GetSHA256() (string, error) {
	key := PlatformKey()
	// New format support
	if len(f.Bottle) > 0 {
		if b, ok := f.Bottle[key]; ok {
			if err := validation.ValidateSHA256(b.SHA256); err != nil {
				return "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, err)
			}
			return b.SHA256, nil
		}
	}
	// Fallback to old format
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
	if len(f.URL) == 0 && len(f.Bottle) == 0 && f.Source.URL == "" {
		return fmt.Errorf("formula %q missing required field: url, bottle, or source", f.Name)
	}
	for platform, u := range f.URL {
		if !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("formula %q: URL for %s must use HTTPS: %s", f.Name, platform, u)
		}
	}
	for platform, b := range f.Bottle {
		if !strings.HasPrefix(b.URL, "https://") {
			return fmt.Errorf("formula %q: bottle URL for %s must use HTTPS: %s", f.Name, platform, b.URL)
		}
	}

	if f.Install.Type == "" && len(f.Build.Configure) == 0 && len(f.Build.Install) == 0 {
		if len(f.Bottle) > 0 {
			f.Install.Type = "archive"
			f.Install.StripComponents = 2 // Most homebrew bottles extract to `name/version/`
		} else {
			return fmt.Errorf("formula %q missing required field: install.type or build configuration", f.Name)
		}
	}

	if f.Install.Type != "" && f.Install.Type != "binary" && f.Install.Type != "archive" {
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
