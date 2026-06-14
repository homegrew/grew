// Package config provides the central path management and configuration discovery
// for homegrew. It handles determining the installation prefix, user-specific
// directories, and ensuring the environment is correctly initialized.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/homegrew/grew/pkg/safepath"
)

// Paths holds the absolute, cleaned paths for all major directories used by grew.
// All fields are guaranteed to be absolute paths after initialization via
// Default() or FromRoot().
type Paths struct {
	// Root is the installation prefix (e.g. /opt/homegrew).
	Root string
	// Cellar is the directory where formulas are installed (Root/Cellar).
	Cellar string
	// Opt is the directory for symlinks to active formula versions (Root/opt).
	Opt string
	// Bin is the directory for executable binaries (Root/bin).
	Bin string
	// Sbin is the directory for system binaries (Root/sbin).
	Sbin string
	// Lib is the directory for libraries (Root/lib).
	Lib string
	// Include is the directory for header files (Root/include).
	Include string
	// Share is the directory for shared data (Root/share).
	Share string
	// Taps is the directory for git repositories of formulas (Root/Taps).
	Taps string
	// CoreTap is the path to the official core formula tap.
	CoreTap string
	// CaskTap is the path to the official cask tap.
	CaskTap string
	// Caskroom is the directory where casks are recorded (Root/Caskroom).
	Caskroom string
	// AppDir is the directory for macOS applications (usually /Applications or ~/Applications).
	AppDir string
	// FontDir is the directory for installed font cask artifacts (usually ~/Library/Fonts).
	FontDir string
	// Cache is the directory for download and metadata cache.
	Cache string
	// Var is the shared variable-data directory (Root/var).
	Var string
	// Tmp is the temporary directory for builds and extractions (Root/tmp).
	Tmp string
	// Log is the directory for audit logs (Root/var/log).
	Log string
	// Locks is the directory for lock files (Root/var/homegrew/locks).
	Locks string
	// Etc is the directory for configuration files and trusted keys (Root/etc).
	Etc string
	// GitRepo is the internal directory for grew's own source (Root/Grew).
	GitRepo string
}

// DefaultPrefix determines the homegrew prefix by following a set of discovery rules.
//
// Discovery rules (in order):
//  1. HOMEGREW_PREFIX environment variable (explicit override).
//  2. Inferred from binary location: if the executable is at <prefix>/bin/grew,
//     the prefix is <prefix>.
//  3. Fallback: platform system prefix for root, or ~/.homegrew for non-root.
func DefaultPrefix() string {
	var prefix string

	if env := os.Getenv("HOMEGREW_PREFIX"); env != "" {
		// Only accept absolute, well-formed prefixes from the environment.
		// absCleanIfValid resolves and cleans first, so valid-but-unclean values
		// (e.g. trailing slashes) are still accepted.
		if clean, err := absCleanIfValid(env); err == nil {
			prefix = clean
		} else {
			slog.Warn(fmt.Sprintf("config: ignoring invalid HOMEGREW_PREFIX %q: %v", env, err))
		}
	}

	if prefix == "" {
		if p, ok := inferPrefixFromExe(); ok {
			prefix = p
		}
	}

	if prefix == "" {
		// Fallback depends on privilege level. Root users always get the
		// system prefix. Non-root users get ~/.homegrew — this path is
		// only reachable in devmode builds with --unsafe; production
		// builds reject non-root earlier in runtime.Init.
		if os.Geteuid() == 0 {
			prefix = systemPrefix()
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			prefix = filepath.Join(home, ".homegrew")
		}
	}

	// Normalize and validate the final prefix before returning.
	if abs, err := filepath.Abs(prefix); err == nil {
		prefix = filepath.Clean(abs)
	} else {
		prefix = filepath.Clean(prefix)
	}
	if err := safepath.SafeAbsolutePath(prefix); err != nil {
		fallback := filepath.Clean(systemPrefix())
		if ferr := safepath.SafeAbsolutePath(fallback); ferr == nil {
			slog.Warn(fmt.Sprintf("config: invalid resolved prefix %q: %v; using %q", prefix, err, fallback))
			return fallback
		}
		slog.Warn(fmt.Sprintf("config: invalid resolved prefix %q: %v", prefix, err))
	}
	return prefix
}

// inferPrefixFromExe infers the homegrew prefix from the running executable's
// location: if the binary is at <prefix>/bin/grew and <prefix> has both a Cellar
// and a Taps directory, it returns (<prefix>, true). Otherwise it returns ("", false).
func inferPrefixFromExe() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(exe) // <prefix>/bin
	if filepath.Base(dir) != "bin" {
		return "", false
	}
	candidate := filepath.Dir(dir) // <prefix>
	// Sanity check: the candidate should have a Cellar AND Taps dir.
	// We use && instead of || to avoid incorrectly adopting a Homebrew prefix
	// (which has a Cellar, but no top-level Taps directory).
	if !IsDir(filepath.Join(candidate, "Cellar")) || !IsDir(filepath.Join(candidate, "Taps")) {
		return "", false
	}
	if clean, err := absCleanIfValid(candidate); err == nil {
		return clean, true
	}
	return "", false
}

// systemPrefix returns the platform system prefix (same logic as runtime.SystemPrefix).
// Duplicated here to avoid a circular dependency between config and runtime.
func systemPrefix() string {
	if goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64" {
		return "/opt/homegrew"
	}
	return "/usr/local/homegrew"
}

// absClean resolves path to an absolute, cleaned form. If filepath.Abs fails
// (effectively never for the inputs here) it cleans the original path instead.
func absClean(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// absCleanIfValid returns absClean(path) when the result is a valid absolute
// path, otherwise ("", err) describing why the path was rejected.
func absCleanIfValid(path string) (string, error) {
	clean := absClean(path)
	if err := safepath.SafeAbsolutePath(clean); err != nil {
		return "", err
	}
	return clean, nil
}

// normalizeDir resolves dir to an absolute, cleaned path. If the result is not a
// valid absolute path, it returns absClean(fallback); when label is non-empty it
// also logs a warning naming label, the offending value, and the fallback used.
func normalizeDir(dir, fallback, label string) string {
	clean := absClean(dir)
	if err := safepath.SafeAbsolutePath(clean); err != nil {
		fb := absClean(fallback)
		if label != "" {
			slog.Warn(fmt.Sprintf("config: invalid %s %q: %v; falling back to %q", label, clean, err, fb))
		}
		return fb
	}
	return clean
}

// Default returns the default Paths for the current environment.
// It discovers the prefix, application directory, and cache directory
// using environment variables and system defaults.
func Default() Paths {
	root := DefaultPrefix()
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	if abs, err := filepath.Abs(home); err == nil {
		home = filepath.Clean(abs)
	} else {
		home = filepath.Clean(home)
	}
	// FromRoot owns all appDir normalization and fallback, so Default only needs
	// to supply the default when the override is unset.
	appDir := os.Getenv("HOMEGREW_APPDIR")
	if appDir == "" {
		appDir = filepath.Join(home, "Applications")
	}

	cacheDir := os.Getenv("HOMEGREW_CACHE")
	if cacheDir == "" {
		if ucd, err := os.UserCacheDir(); err == nil {
			cacheDir = filepath.Join(ucd, "Homegrew")
		} else {
			cacheDir = filepath.Join(home, ".cache", "homegrew")
		}
	}

	return FromRoot(root, appDir, cacheDir)
}

// IsUnderRoot reports whether the given path is located within the Root directory.
// It is used as a safety check before performing destructive operations such as
// recursive deletion.
func (p Paths) IsUnderRoot(path string) bool {
	if p.Root == "" || path == "" {
		return false
	}
	return safepath.IsSubpath(p.Root, path)
}

// resolveFontDir determines where font cask artifacts are installed.
// HOMEGREW_FONTDIR overrides it; otherwise it defaults to Library/Fonts under
// the same home directory that holds appDir. Deriving from appDir keeps fonts
// co-located with apps, so a test or devmode override of HOMEGREW_APPDIR also
// keeps fonts out of the real ~/Library/Fonts.
func resolveFontDir(appDir string) string {
	if env := os.Getenv("HOMEGREW_FONTDIR"); env != "" {
		if cleaned, err := absCleanIfValid(env); err == nil {
			return cleaned
		} else {
			slog.Warn(fmt.Sprintf("config: ignoring invalid HOMEGREW_FONTDIR %q: %v", env, err))
		}
	}

	home, herr := os.UserHomeDir()
	if herr != nil {
		home = "."
	}
	fontDir := filepath.Join(filepath.Dir(appDir), "Library", "Fonts")
	return normalizeDir(fontDir, filepath.Join(home, "Library", "Fonts"), "")
}

// FromRoot builds a Paths struct from an explicit root, appDir, and cacheDir.
func FromRoot(root, appDir, cacheDir string) Paths {
	// Normalize root so that all derived paths (Cellar, Caskroom, etc.) are
	// absolute and cleaned, regardless of how root was provided.
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	root = filepath.Clean(root)
	if err := safepath.SafeAbsolutePath(root); err != nil {
		slog.Warn(fmt.Sprintf("config: invalid root %q: %v; falling back to system prefix", root, err))
		root = filepath.Clean(systemPrefix())
		if abs, err := filepath.Abs(root); err == nil {
			root = filepath.Clean(abs)
		}
		if err := safepath.SafeAbsolutePath(root); err != nil {
			slog.Warn(fmt.Sprintf("config: system prefix %q is invalid: %v", root, err))
		}
	}

	// Normalize appDir and cacheDir so they are absolute and cleaned, regardless
	// of whether they came from the environment or a default; normalizeDir owns
	// the fallback policy and warning.
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		home = "."
	}
	appDir = normalizeDir(appDir, filepath.Join(home, "Applications"), "app dir")
	cacheDir = normalizeDir(cacheDir, filepath.Join(home, ".cache", "homegrew"), "cache dir")

	fontDir := resolveFontDir(appDir)
	varDir := filepath.Join(root, "var")

	return Paths{
		Root:     root,
		Cellar:   filepath.Join(root, "Cellar"),
		Opt:      filepath.Join(root, "opt"),
		Bin:      filepath.Join(root, "bin"),
		Sbin:     filepath.Join(root, "sbin"),
		Lib:      filepath.Join(root, "lib"),
		Include:  filepath.Join(root, "include"),
		Share:    filepath.Join(root, "share"),
		Taps:     filepath.Join(root, "Taps"),
		CoreTap:  filepath.Join(root, "Taps", "homegrew", "homegrew-taps", "core"),
		CaskTap:  filepath.Join(root, "Taps", "homegrew", "homegrew-taps", "cask"),
		Caskroom: filepath.Join(root, "Caskroom"),
		AppDir:   appDir,
		FontDir:  fontDir,
		Cache:    cacheDir,
		Var:      varDir,
		Tmp:      filepath.Join(varDir, "homegrew", "tmp"),
		Log:      filepath.Join(varDir, "homegrew", "log"),
		Locks:    filepath.Join(varDir, "homegrew", "locks"),
		Etc:      filepath.Join(root, "etc"),
		GitRepo:  filepath.Join(root, "Grew"),
	}
}

// NamedDir pairs a human-readable label with an absolute directory path.
type NamedDir struct {
	Name string
	Path string
	// External marks a directory that legitimately lives outside Root (user-scoped),
	// exempt from the under-Root check in Init.
	External bool
}

// InitDirs returns the ordered set of directories that Init creates. It is the
// single source of truth for both Init and dry-run output. p.Share and
// p.GitRepo are intentionally excluded — they must not be created by grew.
func (p Paths) InitDirs() []NamedDir {
	return []NamedDir{
		{Name: "Root", Path: p.Root},
		{Name: "Cellar", Path: p.Cellar},
		{Name: "opt", Path: p.Opt},
		{Name: "bin", Path: p.Bin},
		{Name: "sbin", Path: p.Sbin},
		{Name: "lib", Path: p.Lib},
		{Name: "include", Path: p.Include},
		{Name: "Taps", Path: p.Taps},
		{Name: "CoreTap", Path: p.CoreTap},
		{Name: "CaskTap", Path: p.CaskTap},
		{Name: "Caskroom", Path: p.Caskroom},
		{Name: "AppDir", Path: p.AppDir, External: true},
		{Name: "Cache", Path: p.Cache, External: true},
		{Name: "var", Path: p.Var},
		{Name: "tmp", Path: p.Tmp},
		{Name: "log", Path: p.Log},
		{Name: "locks", Path: p.Locks},
		{Name: "etc", Path: p.Etc},
	}
}

// Init ensures that all required directories in the Paths struct exist on disk.
// It creates them with 0755 permissions. It returns an error if any directory
// cannot be created or if a path would escape the Root prefix.
func (p Paths) Init() error {
	if err := safepath.SafeAbsolutePath(p.Root); err != nil {
		return fmt.Errorf("invalid root path %q: %w", p.Root, err)
	}

	for _, nd := range p.InitDirs() {
		d := nd.Path
		if err := safepath.SafeAbsolutePath(d); err != nil {
			return fmt.Errorf("invalid directory path %q: %w", d, err)
		}
		if !nd.External && !p.IsUnderRoot(nd.Path) {
			return fmt.Errorf("refusing to create directory outside root: %s (root: %s)", d, p.Root)
		}
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", d, err)
		}
	}
	return nil
}

// IsDir reports whether path is an existing directory.
func IsDir(path string) bool {
	if path == "" {
		return false
	}
	if err := safepath.SafeAbsolutePath(path); err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
