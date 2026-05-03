package formula

import (
	"fmt"
	"github.com/homegrew/grew/pkg/safepath"
	"os"
	"path/filepath"

	"strings"
)

type Loader struct {
	TapDir   string
	DebugLog func(format string, args ...any) // optional debug logger
}

// NewLoader constructs a Loader with a normalized, absolute TapDir.
func NewLoader(tapDir string) *Loader {
	if abs, err := filepath.Abs(tapDir); err == nil {
		tapDir = abs
	}
	tapDir = filepath.Clean(tapDir)
	if err := safepath.SafeAbsolutePath(tapDir); err != nil {
		// Keep an invalid sentinel; callers will receive a proper error when used.
		tapDir = ""
	}
	return &Loader{TapDir: tapDir}
}

func (l *Loader) debugf(format string, args ...any) {
	if l.DebugLog != nil {
		l.DebugLog(format, args...)
	}
}

// safeTapPath resolves and validates p, ensuring it remains within l.TapDir.
func (l *Loader) safeTapPath(p string) (string, error) {
	base := filepath.Clean(l.TapDir)
	if abs, err := filepath.Abs(base); err == nil {
		base = filepath.Clean(abs)
	}
	if err := safepath.SafeAbsolutePath(base); err != nil {
		return "", fmt.Errorf("invalid taps directory %q: %w", base, err)
	}

	resolved := filepath.Clean(p)
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = filepath.Clean(abs)
	}
	if err := safepath.SafeAbsolutePath(resolved); err != nil {
		return "", fmt.Errorf("invalid tap path %q: %w", resolved, err)
	}

	rel, err := filepath.Rel(base, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve tap path %q relative to %q: %w", resolved, base, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("tap path %q escapes taps directory %q", resolved, base)
	}

	return resolved, nil
}

func (l *Loader) LoadByName(name string) (*Formula, error) {
	name = strings.TrimSuffix(name, ".yaml")

	// Handle tap-qualified names (e.g., "user/repo/name" or "core/name")
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		formulaName := parts[len(parts)-1]
		tapPath := parts[:len(parts)-1]

		// Validate components
		if err := safepath.SafePathComponent(formulaName + ".yaml"); err != nil {
			return nil, fmt.Errorf("invalid formula name: %q", formulaName)
		}
		for _, p := range tapPath {
			if err := safepath.SafePathComponent(p); err != nil {
				return nil, fmt.Errorf("invalid tap name component: %q", p)
			}
		}

		// Support both homegrew/homegrew-taps and legacy core/cask references
		var paths []string
		if len(tapPath) == 1 {
			if tapPath[0] == "core" || tapPath[0] == "cask" {
				// Redirect core/foo to homegrew/homegrew-taps/core/foo
				paths = append(paths, filepath.Join(l.TapDir, "homegrew", "homegrew-taps", tapPath[0], formulaName+".yaml"))
			}
		}
		paths = append(paths, filepath.Join(append([]string{l.TapDir}, append(tapPath, formulaName+".yaml")...)...))

		for _, path := range paths {
			f, err := l.loadFromFile(path)
			if err == nil {
				return f, nil
			}
		}
		return nil, fmt.Errorf("formula not found in tap %q: %q", strings.Join(tapPath, "/"), formulaName)
	}

	if err := safepath.SafePathComponent(name + ".yaml"); err != nil {
		return nil, fmt.Errorf("invalid formula name: %q", name)
	}
	users, err := os.ReadDir(l.TapDir)
	if err != nil {
		return nil, fmt.Errorf("read taps directory: %w", err)
	}

	var lastErr error
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(user.Name()); err != nil {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(l.TapDir, user.Name()))
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			if err := safepath.SafePathComponent(repo.Name()); err != nil {
				continue
			}

			// Check repo root, repo/core, and repo/Formula subdirectories
			searchPaths := []string{
				filepath.Join(l.TapDir, user.Name(), repo.Name(), name+".yaml"),
				filepath.Join(l.TapDir, user.Name(), repo.Name(), "core", name+".yaml"),
				filepath.Join(l.TapDir, user.Name(), repo.Name(), "Formula", name+".yaml"),
			}

			for _, path := range searchPaths {
				f, err := l.loadFromFile(path)
				if err == nil {
					return f, nil
				}
				if !os.IsNotExist(err) {
					lastErr = fmt.Errorf("failed to parse %s: %w", path, err)
				}
			}
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("formula not found: %q (%v)", name, lastErr)
	}
	return nil, fmt.Errorf("formula not found: %q", name)
}

func (l *Loader) LoadAll() ([]*Formula, error) {
	var formulas []*Formula

	tapDir, err := l.safeTapPath(l.TapDir)
	if err != nil {
		return nil, err
	}

	users, err := os.ReadDir(tapDir)
	if err != nil {
		return nil, fmt.Errorf("read taps directory: %w", err)
	}
	for _, user := range users {
		if !user.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(user.Name()); err != nil {
			continue
		}
		repos, err := os.ReadDir(filepath.Join(tapDir, user.Name()))
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}
			if err := safepath.SafePathComponent(repo.Name()); err != nil {
				continue
			}
			repoPath, err := l.safeTapPath(filepath.Join(tapDir, user.Name(), repo.Name()))
			if err != nil {
				continue
			}
			repoFormulas, err := l.LoadFromTap(repoPath)
			if err != nil {
				continue
			}
			formulas = append(formulas, repoFormulas...)
		}
	}
	return formulas, nil
}

func (l *Loader) LoadFromTap(tapPath string) ([]*Formula, error) {
	absTapPath, err := filepath.Abs(tapPath)
	if err != nil {
		return nil, fmt.Errorf("invalid tap path %q: %w", tapPath, err)
	}
	absTapPath = filepath.Clean(absTapPath)

	if err := safepath.SafeAbsolutePath(absTapPath); err != nil {
		return nil, fmt.Errorf("invalid tap path %q: %w", absTapPath, err)
	}

	tapDir := filepath.Clean(l.TapDir)
	if abs, err := filepath.Abs(tapDir); err == nil {
		tapDir = filepath.Clean(abs)
	}
	if err := safepath.SafeAbsolutePath(tapDir); err != nil {
		return nil, fmt.Errorf("invalid taps directory %q: %w", tapDir, err)
	}
	if err := safepath.CheckSubpath(tapDir, absTapPath); err != nil {
		return nil, fmt.Errorf("tap path %q escapes taps directory %q: %w", absTapPath, tapDir, err)
	}

	// Search in root, Formula/, and core/ subdirectories
	subdirs := []string{"", "Formula", "core"}
	var formulas []*Formula
	seen := make(map[string]bool)

	for _, sub := range subdirs {
		dir := filepath.Join(absTapPath, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			if seen[e.Name()] {
				continue
			}
			if err := safepath.SafePathComponent(e.Name()); err != nil {
				continue
			}
			f, err := l.loadFromFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			formulas = append(formulas, f)
			seen[e.Name()] = true
		}
	}
	return formulas, nil
}

func (l *Loader) loadFromFile(path string) (*Formula, error) {
	// Resolve to an absolute, cleaned path.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absPath = filepath.Clean(absPath)
	if err := safepath.SafeAbsolutePath(absPath); err != nil {
		return nil, fmt.Errorf("invalid formula path %q: %w", absPath, err)
	}

	// Ensure the file we are about to read is within the TapDir tree.
	tapDir := filepath.Clean(l.TapDir)
	if abs, err := filepath.Abs(tapDir); err == nil {
		tapDir = filepath.Clean(abs)
	}
	if err := safepath.SafeAbsolutePath(tapDir); err != nil {
		return nil, fmt.Errorf("invalid taps directory %q: %w", tapDir, err)
	}
	if err := safepath.CheckSubpath(tapDir, absPath); err != nil {
		return nil, fmt.Errorf("formula path %q escapes taps directory %q: %w", absPath, tapDir, err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	f, err := Parse(data)
	if err != nil {
		return nil, err
	}

	// Infer tap name from path (e.g. Taps/user/repo/...)
	rel, err := filepath.Rel(tapDir, absPath)
	if err == nil {
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 2 {
			f.Tap = parts[0] + "/" + parts[1]
		}
	}

	return f, nil
}
