package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
}

// DefaultPrefix determines the grew prefix using these rules (in order):
//
//  1. HOMEGREW_PREFIX env var (explicit override, if valid)
//  2. Inferred from the binary's own location: if the executable lives at
//     <prefix>/bin/grew, the prefix is <prefix>. This means grew always
//     knows where it is without any configuration.
//  3. Fallback to ~/.grew
func DefaultPrefix() string {
	var prefix string

	if env := os.Getenv("HOMEGREW_PREFIX"); env != "" {
		// Only accept absolute, well‑formed prefixes from the environment.
		if filepath.IsAbs(env) {
			if abs, err := filepath.Abs(env); err == nil {
				prefix = filepath.Clean(abs)
			}
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
		// Fallback to user-local.
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		prefix = filepath.Join(home, ".homegrew")
	}

	// Normalize the prefix to an absolute, cleaned path to avoid surprises downstream.
	if abs, err := filepath.Abs(prefix); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(prefix)
}

// SystemPrefix returns the recommended system-level prefix for the current
// platform. Used by `grew setup` when running with sudo.
//
//   - macOS ARM64 (Apple Silicon): /opt/homegrew
//   - macOS AMD64 (Intel):         /usr/local/homegrew
//   - Linux:                        /usr/local/homegrew
func SystemPrefix() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "/opt/homegrew"
	}
	return "/usr/local/homegrew"
}

// UserPrefix returns the user-local prefix (~/.homegrew).
// Used by `grew setup` when running without sudo.
func UserPrefix() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	if abs, err := filepath.Abs(home); err == nil {
		home = filepath.Clean(abs)
	} else {
		home = filepath.Clean(home)
	}
	prefix := filepath.Join(home, ".homegrew")
	if abs, err := filepath.Abs(prefix); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(prefix)
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
	}
}

func (p Paths) Init() error {
	dirs := []string{
		p.Root, p.Cellar, p.Opt, p.Bin, p.Lib,
		p.Include, p.Taps, p.CoreTap, p.CaskTap,
		p.Caskroom, p.AppDir, p.Tmp, p.Log,
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
