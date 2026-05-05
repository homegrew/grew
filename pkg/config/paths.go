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
	"strings"

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
	// Cache is the directory for download and metadata cache.
	Cache string
	// Tmp is the temporary directory for builds and extractions (Root/tmp).
	Tmp string
	// Log is the directory for audit logs (Root/var/log).
	Log string
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
		// We resolve and clean the path first to allow valid-but-unclean values (e.g. trailing slashes).
		if abs, err := filepath.Abs(env); err == nil {
			clean := filepath.Clean(abs)
			if err := safepath.SafeAbsolutePath(clean); err == nil {
				prefix = clean
			} else {
				slog.Warn(fmt.Sprintf("config: ignoring invalid HOMEGREW_PREFIX %q: %v", env, err))
			}
		} else {
			slog.Warn(fmt.Sprintf("config: ignoring invalid HOMEGREW_PREFIX %q: %v", env, err))
		}
	}

	if prefix == "" {
		// Infer from binary location: /opt/homegrew/bin/grew → /opt/homegrew
		if exe, err := os.Executable(); err == nil {
			exe, err = filepath.EvalSymlinks(exe)
			if err == nil {
				dir := filepath.Dir(exe) // <prefix>/bin
				if filepath.Base(dir) == "bin" {
					candidate := filepath.Dir(dir) // <prefix>
					// Sanity check: the candidate should have a Cellar AND Taps dir.
					// We use && instead of || to avoid incorrectly adopting a Homebrew prefix
					// (which has a Cellar, but no top-level Taps directory).
					if IsDir(filepath.Join(candidate, "Cellar")) && IsDir(filepath.Join(candidate, "Taps")) {
						if abs, err := filepath.Abs(candidate); err == nil {
							clean := filepath.Clean(abs)
							if err := safepath.SafeAbsolutePath(clean); err == nil {
								prefix = clean
							}
						} else {
							clean := filepath.Clean(candidate)
							if err := safepath.SafeAbsolutePath(clean); err == nil {
								prefix = clean
							}
						}
					}
				}
			}
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
		if err := safepath.SafeAbsolutePath(fallback); err == nil {
			slog.Warn(fmt.Sprintf("config: invalid resolved prefix %q: %v; using %q", prefix, err, fallback))
			return fallback
		}
		slog.Warn(fmt.Sprintf("config: invalid resolved prefix %q: %v", prefix, err))
	}
	return prefix
}

// systemPrefix returns the platform system prefix (same logic as runtime.SystemPrefix).
// Duplicated here to avoid a circular dependency between config and runtime.
func systemPrefix() string {
	if goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64" {
		return "/opt/homegrew"
	}
	return "/usr/local/homegrew"
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
	appDir := os.Getenv("HOMEGREW_APPDIR")
	if appDir != "" {
		// Both relative and absolute paths are accepted; relative paths are resolved
		// to an absolute, cleaned path. If the value cannot be resolved, it is ignored.
		if abs, err := filepath.Abs(appDir); err == nil {
			appDir = filepath.Clean(abs)
		} else {
			// If the override cannot be resolved to an absolute path, warn and ignore it.
			slog.Warn(fmt.Sprintf("config: ignoring invalid HOMEGREW_APPDIR %q: %v", appDir, err))
			appDir = ""
		}
	}
	if appDir == "" {
		appDir = filepath.Join(home, "Applications")
	}

	if err := safepath.SafeAbsolutePath(appDir); err != nil {
		slog.Warn(fmt.Sprintf("config: invalid app dir %q: %v; falling back to default", appDir, err))
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

	rootAbs, err := filepath.Abs(p.Root)
	if err != nil {
		return false
	}
	rootAbs = filepath.Clean(rootAbs)

	targetAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	targetAbs = filepath.Clean(targetAbs)

	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
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

	// Normalize appDir so that it is also absolute and cleaned, regardless
	// of whether it came from the environment or a default.
	if abs, err := filepath.Abs(appDir); err == nil {
		appDir = abs
	}
	appDir = filepath.Clean(appDir)
	if err := safepath.SafeAbsolutePath(appDir); err != nil {
		invalidAppDir := appDir
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			home = "."
		}
		fallback := filepath.Join(home, "Applications")
		if abs, err := filepath.Abs(fallback); err == nil {
			fallback = abs
		}
		appDir = filepath.Clean(fallback)
		slog.Warn(fmt.Sprintf("config: invalid app dir %q: %v; falling back to %q", invalidAppDir, err, appDir))
	}

	// Normalize cacheDir so that it is also absolute and cleaned.
	if abs, err := filepath.Abs(cacheDir); err == nil {
		cacheDir = abs
	}
	cacheDir = filepath.Clean(cacheDir)
	if err := safepath.SafeAbsolutePath(cacheDir); err != nil {
		invalidCacheDir := cacheDir
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			home = "."
		}
		fallback := filepath.Join(home, ".cache", "homegrew")
		if abs, err := filepath.Abs(fallback); err == nil {
			fallback = abs
		}
		cacheDir = filepath.Clean(fallback)
		slog.Warn(fmt.Sprintf("config: invalid cache dir %q: %v; falling back to %q", invalidCacheDir, err, cacheDir))
	}

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
		Cache:    cacheDir,
		Tmp:      filepath.Join(root, "tmp"),
		Log:      filepath.Join(root, "var", "log"),
		GitRepo:  filepath.Join(root, "Grew"),
	}
}

// Init ensures that all required directories in the Paths struct exist on disk.
// It creates them with 0755 permissions. It returns an error if any directory
// cannot be created or if a path would escape the Root prefix.
func (p Paths) Init() error {
	if err := safepath.SafeAbsolutePath(p.Root); err != nil {
		return fmt.Errorf("invalid root path %q: %w", p.Root, err)
	}

	dirs := []string{
		p.Root, p.Cellar, p.Opt, p.Bin, p.Sbin, p.Lib,
		p.Include, p.Taps,
		p.Caskroom, p.AppDir, p.Cache, p.Tmp, p.Log, // p.GitRepo must not be created, p.Share must not be created
	}
	for _, d := range dirs {
		if err := safepath.SafeAbsolutePath(d); err != nil {
			return fmt.Errorf("invalid directory path %q: %w", d, err)
		}
		if d != p.AppDir && d != p.Cache && !p.IsUnderRoot(d) {
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
