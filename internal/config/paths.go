package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/homegrew/grew/pkg/validation"
)

type Paths struct {
	Root     string
	Cellar   string
	Opt      string
	Bin      string
	Lib      string
	Include  string
	Taps     string
	CoreTap  string
	CaskTap  string
	Caskroom string
	AppDir   string
	Tmp      string
	Log      string
	GitRepo  string
}

// DefaultPrefix determines the homegrew prefix using these rules (in order):
//
//  1. HOMEGREW_PREFIX env var (explicit override, if valid)
//  2. Inferred from the binary's own location: if the executable lives at
//     <prefix>/bin/grew, the prefix is <prefix>. This means grew always
//     knows where it is without any configuration.
//  3. Fallback: system prefix for root, ~/.homegrew for non-root with GREW_DEVMODE=1
func DefaultPrefix() string {
	var prefix string

	if env := os.Getenv("HOMEGREW_PREFIX"); env != "" {
		// Only accept absolute, well-formed prefixes from the environment.
		if err := validation.SafeAbsolutePath(env); err == nil {
			prefix = env
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
					// Sanity check: the candidate should have a Cellar or Taps dir.
					if IsDir(filepath.Join(candidate, "Cellar")) || IsDir(filepath.Join(candidate, "Taps")) {
						if abs, err := filepath.Abs(candidate); err == nil {
							prefix = filepath.Clean(abs)
						} else {
							prefix = filepath.Clean(candidate)
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

	// Normalize the prefix to an absolute, cleaned path to avoid surprises downstream.
	if abs, err := filepath.Abs(prefix); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(prefix)
}

// systemPrefix returns the platform system prefix (same logic as runtime.SystemPrefix).
// Duplicated here to avoid a circular dependency between config and runtime.
func systemPrefix() string {
	if goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64" {
		return "/opt/homegrew"
	}
	return "/usr/local/homegrew"
}

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

	return FromRoot(root, appDir)
}

// IsUnderRoot reports whether the given path is located within the Paths.Root
// directory. It is used as a safety check before performing destructive
// operations such as recursive deletion.
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

// FromRoot builds a Paths struct from an explicit root and appDir.
func FromRoot(root, appDir string) Paths {
	// Normalize root so that all derived paths (Cellar, Caskroom, etc.) are
	// absolute and cleaned, regardless of how root was provided.
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	root = filepath.Clean(root)

	// Normalize appDir so that it is also absolute and cleaned, regardless
	// of whether it came from the environment or a default.
	if abs, err := filepath.Abs(appDir); err == nil {
		appDir = abs
	}
	appDir = filepath.Clean(appDir)

	return Paths{
		Root:     root,
		Cellar:   filepath.Join(root, "Cellar"),
		Opt:      filepath.Join(root, "opt"),
		Bin:      filepath.Join(root, "bin"),
		Lib:      filepath.Join(root, "lib"),
		Include:  filepath.Join(root, "include"),
		Taps:     filepath.Join(root, "Taps"),
		CoreTap:  filepath.Join(root, "Taps", "core"),
		CaskTap:  filepath.Join(root, "Taps", "cask"),
		Caskroom: filepath.Join(root, "Caskroom"),
		AppDir:   appDir,
		Tmp:      filepath.Join(root, "tmp"),
		Log:      filepath.Join(root, "var", "log"),
		GitRepo:  filepath.Join(root, "Grew"),
	}
}

func (p Paths) Init() error {
	dirs := []string{
		p.Root, p.Cellar, p.Opt, p.Bin, p.Lib,
		p.Include, p.Taps, p.CoreTap, p.CaskTap,
		p.Caskroom, p.AppDir, p.Tmp, p.Log, // p.GitRepo must not be created
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", d, err)
		}
	}
	return nil
}

// IsDir reports whether path is an existing directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
