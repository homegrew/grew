package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
}

// DefaultPrefix determines the grew prefix using these rules (in order):
//
//  1. HOMEGREW_PREFIX env var (explicit override, always wins)
//  2. Inferred from the binary's own location: if the executable lives at
//     <prefix>/bin/grew, the prefix is <prefix>. This means grew always
//     knows where it is without any configuration.
//  3. Fallback to ~/.grew
func DefaultPrefix() string {
	if env := os.Getenv("HOMEGREW_PREFIX"); env != "" {
		return env
	}

	// Infer from binary location: /opt/grew/bin/grew → /opt/grew
	if exe, err := os.Executable(); err == nil {
		exe, err = filepath.EvalSymlinks(exe)
		if err == nil {
			dir := filepath.Dir(exe)                    // <prefix>/bin
			if filepath.Base(dir) == "bin" {
				candidate := filepath.Dir(dir)          // <prefix>
				// Sanity check: the candidate should have a Cellar or Taps dir.
				if IsDir(filepath.Join(candidate, "Cellar")) || IsDir(filepath.Join(candidate, "Taps")) {
					return candidate
				}
			}
		}
	}

	// Fallback to user-local.
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".grew")
}

// SystemPrefix returns the recommended system-level prefix for the current
// platform. Used by `grew setup` when running with sudo.
//
//   - macOS ARM64 (Apple Silicon): /opt/grew
//   - macOS AMD64 (Intel):         /usr/local/grew
//   - Linux:                        /usr/local/grew
func SystemPrefix() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "/opt/grew"
	}
	return "/usr/local/grew"
}

// UserPrefix returns the user-local prefix (~/.grew).
// Used by `grew setup` when running without sudo.
func UserPrefix() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".grew")
}

func Default() Paths {
	root := DefaultPrefix()
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	appDir := os.Getenv("HOMEGREW_APPDIR")
	if appDir == "" {
		appDir = filepath.Join(home, "Applications")
	}

	return FromRoot(root, appDir)
}

// FromRoot builds a Paths struct from an explicit root and appDir.
func FromRoot(root, appDir string) Paths {
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
	}
}

func (p Paths) Init() error {
	dirs := []string{
		p.Root, p.Cellar, p.Opt, p.Bin, p.Lib,
		p.Include, p.Taps, p.CoreTap, p.CaskTap,
		p.Caskroom, p.AppDir, p.Tmp,
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
