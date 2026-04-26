package formula

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/validation"
	"gopkg.in/yaml.v3"
)

type SourceSpec struct {
	URL       string `yaml:"url"`
	SHA256    string `yaml:"sha256"`
	SHA512    string `yaml:"sha512"`
	Signature string `yaml:"signature"`
}

type BottleSpec struct {
	URL       string `yaml:"url"`
	SHA256    string `yaml:"sha256"`
	SHA512    string `yaml:"sha512"`
	Signature string `yaml:"signature"`
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
	SHA512       map[string]string `yaml:"sha512"`
	Signature    map[string]string `yaml:"signature"`
	SourceURL    string            `yaml:"source_url"`
	SourceSHA256 string            `yaml:"source_sha256"`
	SourceSHA512 string            `yaml:"source_sha512"`
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

var (
	platformKeyOnce sync.Once
	cachedPlatform  string
)

func PlatformKey() string {
	platformKeyOnce.Do(func() {
		osName := runtime.GOOS
		arch := runtime.GOARCH

		if osName == "darwin" {
			// On macOS, we want a key like "arm64_sequoia" or "arm64_sonoma".
			// Homebrew uses these names. For simplicity in grew, we'll start with
			// major version numbers if we can't map to names easily, or just
			// use the generic arm64/amd64 if no versioned bottle exists.
			//
			// However, to match the reported error, we need to know we are on
			// an older OS.
			out, err := exec.Command("sw_vers", "-productVersion").Output()
			if err == nil {
				version := strings.TrimSpace(string(out))
				major := strings.Split(version, ".")[0]
				// We'll use a key like "darwin_arm64_26" or similar.
				// But to stay compatible with existing formulas, we might need
				// to try versioned keys first and then fallback.
				cachedPlatform = fmt.Sprintf("%s_%s_%s", osName, arch, major)
				return
			}
		}
		cachedPlatform = osName + "_" + arch
	})
	return cachedPlatform
}

// GetBottleSpec returns the best matching bottle for the current platform.
func (f *Formula) GetBottleSpec() (BottleSpec, string, bool) {
	key := PlatformKey()
	if b, ok := f.Bottle[key]; ok {
		return b, key, true
	}

	// For Darwin, we don't fallback to generic darwin_arm64 if it might be
	// incompatible. Instead, we return false so the caller can fallback
	// to a source build.
	if strings.HasPrefix(key, "darwin") {
		return BottleSpec{}, "", false
	}

	// Fallback to generic key for other platforms if no versioned key exists.
	genericKey := runtime.GOOS + "_" + runtime.GOARCH
	if b, ok := f.Bottle[genericKey]; ok {
		return b, genericKey, true
	}

	return BottleSpec{}, "", false
}

func (f *Formula) GetURL() (string, error) {
	// New format support
	if len(f.Bottle) > 0 {
		if b, key, ok := f.GetBottleSpec(); ok {
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

// GetSourceSHA512 returns the SHA512 checksum for the source archive.
func (f *Formula) GetSourceSHA512() (string, error) {
	s := ""
	if f.Source.SHA512 != "" {
		s = f.Source.SHA512
	} else {
		s = f.SourceSHA512
	}

	if s == "" {
		return "", nil
	}
	if err := validation.ValidateSHA512(s); err != nil {
		return "", fmt.Errorf("formula %q: invalid source_sha512: %w", f.Name, err)
	}
	return s, nil
}

// GetSHA256 returns the SHA256 checksum for the current platform.
func (f *Formula) GetSHA256() (string, error) {
	// New format support
	if len(f.Bottle) > 0 {
		if b, key, ok := f.GetBottleSpec(); ok {
			if err := validation.ValidateSHA256(b.SHA256); err != nil {
				return "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, err)
			}
			return b.SHA256, nil
		}
	}
	// Fallback to old format
	key := PlatformKey()
	s, ok := f.SHA256[key]
	if !ok {
		return "", fmt.Errorf("formula %q does not support platform %s; available: %s",
			f.Name, key, sortedMapKeys(f.URL))
	}
	if err := validation.ValidateSHA256(s); err != nil {
		return "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, err)
	}
	return s, nil
}

// GetSHA512 returns the SHA512 checksum for the current platform.
func (f *Formula) GetSHA512() (string, error) {
	s := ""
	if len(f.Bottle) > 0 {
		if b, _, ok := f.GetBottleSpec(); ok {
			s = b.SHA512
		}
	} else {
		key := PlatformKey()
		s = f.SHA512[key]
	}

	if s == "" {
		return "", nil
	}
	if err := validation.ValidateSHA512(s); err != nil {
		return "", fmt.Errorf("formula %q: invalid SHA512: %w", f.Name, err)
	}
	return s, nil
}

// GetSignature returns the bottle signature for the current platform, or ""
// if none is set. New-format bottles store it on BottleSpec; legacy formulas
// use the top-level Signature map.
func (f *Formula) GetSignature() string {
	if len(f.Bottle) > 0 {
		if b, _, ok := f.GetBottleSpec(); ok {
			return b.Signature
		}
	}
	key := PlatformKey()
	return f.Signature[key]
}

// GetSourceSignature returns the source signature, or "" if none is set.
func (f *Formula) GetSourceSignature() string {
	return f.Source.Signature
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
	if f.Install.BinaryName != "" {
		if err := safepath.SafePathComponent(f.Install.BinaryName); err != nil {
			return fmt.Errorf("formula %q has invalid binary_name: %w", f.Name, err)
		}
	}
	if f.Install.Format != "" {
		if err := safepath.SafePathComponent(f.Install.Format); err != nil {
			return fmt.Errorf("formula %q has invalid install format: %w", f.Name, err)
		}
	}
	if f.Service != nil {
		if f.Service.WorkingDir != "" && strings.Contains(f.Service.WorkingDir, "..") {
			return fmt.Errorf("formula %q has invalid service working_dir (contains traversals)", f.Name)
		}
		if f.Service.LogPath != "" && strings.Contains(f.Service.LogPath, "..") {
			return fmt.Errorf("formula %q has invalid service log_path (contains traversals)", f.Name)
		}
		if f.Service.ErrorLogPath != "" && strings.Contains(f.Service.ErrorLogPath, "..") {
			return fmt.Errorf("formula %q has invalid service error_log_path (contains traversals)", f.Name)
		}
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
