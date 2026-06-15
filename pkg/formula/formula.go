package formula

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/validation"
	"gopkg.in/yaml.v3"
)

// DepKind classifies a formula dependency by install phase and optionality.
type DepKind int

const (
	DepRuntime     DepKind = iota // default runtime dependency
	DepBuild                      // required only during build
	DepTest                       // required only during test do
	DepOptional                   // optional runtime dependency
	DepRecommended                // recommended but not required
)

// Dependency is a single declared formula dependency with scope and platform tags.
type Dependency struct {
	Name     string   `yaml:"name"`
	Kind     DepKind  `yaml:"kind,omitempty"`
	Optional bool     `yaml:"optional,omitempty"`
	Tags     []string `yaml:"tags,omitempty"`
}

// FilterByKind returns the subset of deps whose Kind matches kind.
func FilterByKind(deps []Dependency, kind DepKind) []Dependency {
	var out []Dependency
	for _, d := range deps {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

type BottleSpec struct {
	URL       string `yaml:"url,omitempty"`
	SHA256    string `yaml:"sha256,omitempty"`
	SHA512    string `yaml:"sha512,omitempty"`
	Cellar    string `yaml:"cellar,omitempty"`
	Signature string `yaml:"signature,omitempty"`
}

type SourceSpec struct {
	URL       string `yaml:"url,omitempty"`
	SHA256    string `yaml:"sha256,omitempty"`
	SHA512    string `yaml:"sha512,omitempty"`
	Signature string `yaml:"signature,omitempty"`
}

type HeadSpec struct {
	URL    string `yaml:"url,omitempty"`
	Branch string `yaml:"branch,omitempty"`
	Using  string `yaml:"using,omitempty"`
}

type BuildSpec struct {
	// WorkingDir is an optional subdirectory of the extracted source tree in
	// which the configure/make/install steps run. Projects such as tcl-tk keep
	// their configure script under a subdirectory (e.g. "unix"); set this to
	// that path. Empty means the source-tree root. Must be a relative path
	// without ".." traversal.
	WorkingDir string `yaml:"working_dir,omitempty"`
	// Configure overrides the default `./configure --prefix=<keg>` invocation.
	// The first element is the program, the rest its arguments. Use the
	// "{prefix}" placeholder to reference the keg install prefix.
	Configure []string `yaml:"configure,omitempty"`
	// Install overrides the default `make install` invocation. Same argv and
	// "{prefix}" placeholder rules as Configure. The intermediate `make` build
	// step always runs.
	Install []string `yaml:"install,omitempty"`
}

type ArtifactsSpec struct {
	App []string `yaml:"app,omitempty"`
	Pkg []string `yaml:"pkg,omitempty"`
	Bin []string `yaml:"bin,omitempty"`
}

type Formula struct {
	Name         string            `yaml:"name"`
	Version      string            `yaml:"version"`
	Description  string            `yaml:"description"`
	Homepage     string            `yaml:"homepage"`
	License      string            `yaml:"license"`
	Caveats      string            `yaml:"caveats,omitempty"`
	URL          map[string]string `yaml:"url,omitempty"`
	SHA256       map[string]string `yaml:"sha256,omitempty"`
	SHA512       map[string]string `yaml:"sha512,omitempty"`
	Signature    map[string]string `yaml:"signature,omitempty"`
	SourceURL    string            `yaml:"source_url,omitempty"`
	SourceSHA256 string            `yaml:"source_sha256,omitempty"`
	SourceSHA512 string            `yaml:"source_sha512,omitempty"`
	Install      InstallSpec       `yaml:"install,omitempty"`
	PostInstall  string            `yaml:"post_install,omitempty"`
	Dependencies []string          `yaml:"dependencies,omitempty"`
	KegOnly      bool              `yaml:"keg_only,omitempty"`
	// New schema fields
	Bottle            map[string]BottleSpec `yaml:"bottle,omitempty"`
	Source            *SourceSpec           `yaml:"source,omitempty"`
	Head              *HeadSpec             `yaml:"head,omitempty"`
	BuildDependencies []string              `yaml:"build_dependencies,omitempty"`
	Deps              []Dependency          `yaml:"deps,omitempty"`
	BuildHooks        []string              `yaml:"build_hooks,omitempty"`
	TestHook          string                `yaml:"test_hook,omitempty"`
	Service           *ServiceSpec          `yaml:"service,omitempty"`
	Artifacts         ArtifactsSpec         `yaml:"artifacts,omitempty"`
	Build             BuildSpec             `yaml:"build,omitempty"`

	// Local fields (not in YAML)
	Tap string `yaml:"-"`
}

// IsVersioned reports whether the formula name carries an @-version suffix
// (e.g. "node@24", "python@3.12"). Homebrew treats every such formula as
// keg-only so that only the unversioned formula owns the shared binaries.
func (f *Formula) IsVersioned() bool {
	return strings.Contains(f.Name, "@")
}

// EffectiveKegOnly is the authoritative keg-only decision: an explicit
// keg_only: true OR a versioned name. Callers deciding whether to link a
// formula into bin/lib/include/share should use this rather than the raw
// KegOnly field, so a versioned formula is never linked even if its
// definition forgets to set keg_only.
func (f *Formula) EffectiveKegOnly() bool {
	return f.KegOnly || f.IsVersioned()
}

// BaseName returns the formula name with any @-version suffix stripped
// ("node@24" -> "node", "node" -> "node"). Members of a version family share
// a base name and therefore compete for the same linked binaries.
func BaseName(name string) string {
	if i := strings.IndexByte(name, '@'); i >= 0 {
		return name[:i]
	}
	return name
}

type ServiceSpec struct {
	Run          []string `yaml:"run,omitempty"`
	RunType      string   `yaml:"run_type,omitempty"`
	WorkingDir   string   `yaml:"working_dir,omitempty"`
	LogPath      string   `yaml:"log_path,omitempty"`
	ErrorLogPath string   `yaml:"error_log_path,omitempty"`
	KeepAlive    bool     `yaml:"keep_alive,omitempty"`
}

type InstallSpec struct {
	Type            string `yaml:"type,omitempty"` // "binary" or "archive"
	BinaryName      string `yaml:"binary_name,omitempty"`
	StripComponents int    `yaml:"strip_components,omitempty"`
	Format          string `yaml:"format,omitempty"` // optional: "tar.gz", "zip" — used when URL has no extension
}

var (
	platformKeyOnce sync.Once
	cachedPlatform  string
)

// PlatformKey returns the default platform key for the current host.
func PlatformKey() string {
	platformKeyOnce.Do(func() {
		cachedPlatform = GetPlatformKey(runtime.GOOS, runtime.GOARCH)
	})
	return cachedPlatform
}

// GetPlatformKey returns a platform key for the given OS and architecture.
// If osName is "darwin" and matches the current host, it includes the macOS major version.
func GetPlatformKey(osName, arch string) string {
	if osName == "darwin" && runtime.GOOS == "darwin" && arch == runtime.GOARCH {
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err == nil {
			version := strings.TrimSpace(string(out))
			major := strings.Split(version, ".")[0]
			return fmt.Sprintf("%s_%s_%s", osName, arch, major)
		}
	}
	return osName + "_" + arch
}

// GetBottleSpec returns the best matching bottle for the current platform.
func (f *Formula) GetBottleSpec() (BottleSpec, string, bool) {
	return f.GetBottleSpecForPlatform(runtime.GOOS, runtime.GOARCH)
}

// GetBottleSpecForPlatform returns the best matching bottle for the given platform.
func (f *Formula) GetBottleSpecForPlatform(osName, arch string) (BottleSpec, string, bool) {
	key := GetPlatformKey(osName, arch)
	if b, ok := f.Bottle[key]; ok {
		return b, key, true
	}

	// Fallback to generic key for other platforms if no versioned key exists.
	genericKey := osName + "_" + arch
	if b, ok := f.Bottle[genericKey]; ok {
		return b, genericKey, true
	}

	return BottleSpec{}, "", false
}

func (f *Formula) GetURL() (string, error) {
	return f.GetURLForPlatform(runtime.GOOS, runtime.GOARCH)
}

// GetURLForPlatform returns the download URL for the given platform.
func (f *Formula) GetURLForPlatform(osName, arch string) (string, error) {
	// New format support
	if len(f.Bottle) > 0 {
		if b, _, ok := f.GetBottleSpecForPlatform(osName, arch); ok {
			if !strings.HasPrefix(b.URL, "https://") {
				return "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, b.URL)
			}
			return b.URL, nil
		}
	}
	// Fallback to old format
	key := GetPlatformKey(osName, arch)
	u, ok := f.URL[key]
	if !ok {
		genericKey := osName + "_" + arch
		u, ok = f.URL[genericKey]
		if !ok {
			return "", fmt.Errorf("formula %q does not support platform %s; available: %s",
				f.Name, key, sortedMapKeys(f.URL))
		}
	}
	if !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, u)
	}
	return u, nil
}

func (f *Formula) GetSourceURL() (string, error) {
	if f.Source != nil && f.Source.URL != "" {
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
	if f.Source != nil && f.Source.SHA256 != "" {
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
	if f.Source != nil && f.Source.SHA512 != "" {
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

func (f *Formula) GetSHA256() (string, error) {
	return f.GetSHA256ForPlatform(runtime.GOOS, runtime.GOARCH)
}

// GetSHA256ForPlatform returns the SHA256 checksum for the given platform.
func (f *Formula) GetSHA256ForPlatform(osName, arch string) (string, error) {
	// New format support
	if len(f.Bottle) > 0 {
		if b, key, ok := f.GetBottleSpecForPlatform(osName, arch); ok {
			if err := validation.ValidateSHA256(b.SHA256); err != nil {
				return "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, err)
			}
			return b.SHA256, nil
		}
	}
	// Fallback to old format
	key := GetPlatformKey(osName, arch)
	s, ok := f.SHA256[key]
	if !ok {
		genericKey := osName + "_" + arch
		s, ok = f.SHA256[genericKey]
		if !ok {
			return "", fmt.Errorf("formula %q does not support platform %s; available: %s",
				f.Name, key, sortedMapKeys(f.URL))
		}
		key = genericKey
	}
	if err := validation.ValidateSHA256(s); err != nil {
		return "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, err)
	}
	return s, nil
}

func (f *Formula) GetSHA512() (string, error) {
	return f.GetSHA512ForPlatform(runtime.GOOS, runtime.GOARCH)
}

// GetSHA512ForPlatform returns the SHA512 checksum for the given platform.
func (f *Formula) GetSHA512ForPlatform(osName, arch string) (string, error) {
	s := ""
	if len(f.Bottle) > 0 {
		if b, _, ok := f.GetBottleSpecForPlatform(osName, arch); ok {
			s = b.SHA512
		}
	} else {
		key := GetPlatformKey(osName, arch)
		var ok bool
		s, ok = f.SHA512[key]
		if !ok {
			genericKey := osName + "_" + arch
			s = f.SHA512[genericKey]
		}
	}

	if s == "" {
		return "", nil
	}
	if err := validation.ValidateSHA512(s); err != nil {
		return "", fmt.Errorf("formula %q: invalid SHA512: %w", f.Name, err)
	}
	return s, nil
}

// newestVersionKey returns the bottle key for the newest macOS version available
// for the given OS/arch (e.g. the highest N among darwin_arm64_N keys), scanning
// both the new bottle map and the legacy URL map. This backs --force-bottle's
// "newest version of macOS" fallback. Returns ("", false) if no versioned key
// exists for this OS/arch.
func (f *Formula) newestVersionKey(osName, arch string) (string, bool) {
	prefix := osName + "_" + arch + "_"
	best := -1
	bestKey := ""
	consider := func(key string) {
		if !strings.HasPrefix(key, prefix) {
			return
		}
		v, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
		if err != nil {
			return
		}
		if v > best {
			best = v
			bestKey = key
		}
	}
	for k := range f.Bottle {
		consider(k)
	}
	for k := range f.URL {
		consider(k)
	}
	if bestKey == "" {
		return "", false
	}
	return bestKey, true
}

// ResolveForceBottle returns the URL, SHA256 and SHA512 of the bottle selected
// under --force-bottle for the current host. All three values are read from the
// same bottle key so they cannot be mixed across versions.
//
// It mirrors `brew install --force-bottle`: use the bottle for the current macOS
// version if one exists, otherwise the bottle for the newest available macOS
// version for this architecture. It never falls back to a source build — if no
// bottle exists at all it returns an error.
func (f *Formula) ResolveForceBottle() (url, sha256, sha512 string, err error) {
	return f.resolveForceBottle(runtime.GOOS, runtime.GOARCH)
}

func (f *Formula) resolveForceBottle(osName, arch string) (url, sha256, sha512 string, err error) {
	// First try the normal selection (exact current-version key, then generic).
	// GetURL/GetSHA*ForPlatform all resolve through the same key, so the three
	// values stay consistent.
	if u, uerr := f.GetURLForPlatform(osName, arch); uerr == nil {
		s256, serr := f.GetSHA256ForPlatform(osName, arch)
		if serr != nil {
			return "", "", "", serr
		}
		s512, _ := f.GetSHA512ForPlatform(osName, arch)
		return u, s256, s512, nil
	}

	// No bottle for the current macOS version: fall back to the newest available
	// macOS version bottle for this architecture.
	key, ok := f.newestVersionKey(osName, arch)
	if !ok {
		return "", "", "", fmt.Errorf("formula %q: --force-bottle: no bottle available for %s",
			f.Name, GetPlatformKey(osName, arch))
	}

	// New-format bottle.
	if b, ok := f.Bottle[key]; ok {
		if !strings.HasPrefix(b.URL, "https://") {
			return "", "", "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, b.URL)
		}
		if verr := validation.ValidateSHA256(b.SHA256); verr != nil {
			return "", "", "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, verr)
		}
		if b.SHA512 != "" {
			if verr := validation.ValidateSHA512(b.SHA512); verr != nil {
				return "", "", "", fmt.Errorf("formula %q: invalid SHA512 for %s: %w", f.Name, key, verr)
			}
		}
		return b.URL, b.SHA256, b.SHA512, nil
	}

	// Legacy format.
	u, ok := f.URL[key]
	if !ok {
		return "", "", "", fmt.Errorf("formula %q: --force-bottle: no bottle available for %s",
			f.Name, GetPlatformKey(osName, arch))
	}
	if !strings.HasPrefix(u, "https://") {
		return "", "", "", fmt.Errorf("formula %q: refusing to download over insecure HTTP: %s", f.Name, u)
	}
	s256 := f.SHA256[key]
	if verr := validation.ValidateSHA256(s256); verr != nil {
		return "", "", "", fmt.Errorf("formula %q: invalid SHA256 for %s: %w", f.Name, key, verr)
	}
	s512 := f.SHA512[key]
	if s512 != "" {
		if verr := validation.ValidateSHA512(s512); verr != nil {
			return "", "", "", fmt.Errorf("formula %q: invalid SHA512 for %s: %w", f.Name, key, verr)
		}
	}
	return u, s256, s512, nil
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
	if s, ok := f.Signature[key]; ok {
		return s
	}
	genericKey := runtime.GOOS + "_" + runtime.GOARCH
	return f.Signature[genericKey]
}

// GetSourceSignature returns the source signature, or "" if none is set.
func (f *Formula) GetSourceSignature() string {
	if f.Source == nil {
		return ""
	}
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
	if len(f.URL) == 0 && len(f.Bottle) == 0 && (f.Source == nil || f.Source.URL == "") && f.Head == nil {
		return fmt.Errorf("formula %q missing required field: url, bottle, source, or head", f.Name)
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
		if len(f.Artifacts.App) > 0 || len(f.Artifacts.Pkg) > 0 || len(f.Artifacts.Bin) > 0 {
			// Casks don't strictly require install.type or build configuration.
			// The default archive extractor will handle them if needed.
			f.Install.Type = "archive"
		} else if len(f.Bottle) > 0 {
			f.Install.Type = "archive"
			f.Install.StripComponents = 2 // Most homebrew bottles extract to `name/version/`
			if f.Install.Format == "" {
				f.Install.Format = "tar.gz"
			}
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
	if f.Build.WorkingDir != "" && strings.Contains(f.Build.WorkingDir, "..") {
		return fmt.Errorf("formula %q has invalid build working_dir (contains traversals)", f.Name)
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
	for _, dep := range f.Deps {
		if !validation.IsValidName(dep.Name) {
			return fmt.Errorf("formula %q: dep %q contains invalid characters", f.Name, dep.Name)
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
