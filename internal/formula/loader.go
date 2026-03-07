package formula

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Loader struct {
	TapDir   string
	DebugLog func(format string, args ...any) // optional debug logger
}

func (l *Loader) debugf(format string, args ...any) {
	if l.DebugLog != nil {
		l.DebugLog(format, args...)
	}
}

func (l *Loader) LoadByName(name string) (*Formula, error) {
	taps, err := os.ReadDir(l.TapDir)
	if err != nil {
		return nil, fmt.Errorf("read taps directory: %w", err)
	}
	for _, tap := range taps {
		if !tap.IsDir() {
			continue
		}
		f, err := l.loadFromFile(filepath.Join(l.TapDir, tap.Name(), name+".yaml"))
		if err == nil {
			return f, nil
		}
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
	entries, err := os.ReadDir(tapPath)
	if err != nil {
		return nil, fmt.Errorf("read tap %s: %w", tapPath, err)
	}
	var formulas []*Formula
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		f, err := l.loadFromFile(filepath.Join(tapPath, e.Name()))
		if err != nil {
			l.debugf("failed to parse %s: %v\n", e.Name(), err)
			continue
		}
		formulas = append(formulas, f)
	}
	return formulas, nil
}

func (l *Loader) loadFromFile(path string) (*Formula, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}
