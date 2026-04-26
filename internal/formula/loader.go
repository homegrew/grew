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
		tapDir = filepath.Clean(string(filepath.Separator))
	}
	return &Loader{TapDir: tapDir}
}

func (l *Loader) debugf(format string, args ...any) {
	if l.DebugLog != nil {
		l.DebugLog(format, args...)
	}
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

		path := filepath.Join(append([]string{l.TapDir}, append(tapPath, formulaName+".yaml")...)...)
		f, err := l.loadFromFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("formula not found in tap %q: %q", strings.Join(tapPath, "/"), formulaName)
			}
			return nil, fmt.Errorf("failed to load formula %q from tap %q: %w", formulaName, strings.Join(tapPath, "/"), err)
		}
		return f, nil
	}

	if err := safepath.SafePathComponent(name + ".yaml"); err != nil {
		return nil, fmt.Errorf("invalid formula name: %q", name)
	}
	taps, err := os.ReadDir(l.TapDir)
	if err != nil {
		return nil, fmt.Errorf("read taps directory: %w", err)
	}

	var lastErr error
	for _, tap := range taps {
		if !tap.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(tap.Name()); err != nil {
			continue
		}
		path := filepath.Join(l.TapDir, tap.Name(), name+".yaml")
		f, err := l.loadFromFile(path)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) {
			lastErr = fmt.Errorf("failed to parse %s: %w", path, err)
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("formula not found: %q (%v)", name, lastErr)
	}
	return nil, fmt.Errorf("formula not found: %q", name)
}

func (l *Loader) LoadAll() ([]*Formula, error) {
	var formulas []*Formula
	taps, err := os.ReadDir(l.TapDir)
	if err != nil {
		return nil, fmt.Errorf("read taps directory: %w", err)
	}
	for _, tap := range taps {
		if !tap.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(tap.Name()); err != nil {
			continue
		}
		tapFormulas, err := l.LoadFromTap(filepath.Join(l.TapDir, tap.Name()))
		if err != nil {
			l.debugf("failed to load tap %s: %v\n", tap.Name(), err)
			continue
		}
		formulas = append(formulas, tapFormulas...)
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

	entries, err := os.ReadDir(absTapPath)
	if err != nil {
		return nil, fmt.Errorf("read tap %s: %w", absTapPath, err)
	}
	var formulas []*Formula
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if err := safepath.SafePathComponent(e.Name()); err != nil {
			continue
		}
		f, err := l.loadFromFile(filepath.Join(absTapPath, e.Name()))
		if err != nil {
			l.debugf("failed to parse %s: %v\n", e.Name(), err)
			continue
		}
		formulas = append(formulas, f)
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
	return Parse(data)
}
