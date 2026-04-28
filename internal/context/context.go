package context

import (
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/tap"
)

// Context bundles objects used by most commands.
type Context struct {
	Paths      config.Paths
	Loader     *formula.Loader
	CaskLoader *cask.Loader
	Cellar     *cellar.Cellar
	Caskroom   *cask.Caskroom
}

// New initialises paths and the core tap, returning a shared context.
func New() (*Context, error) {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return nil, err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		return nil, fmt.Errorf("init core tap: %w", err)
	}

	return &Context{
		Paths:      paths,
		Loader:     NewLoader(paths.Taps),
		CaskLoader: NewCaskLoader(paths.Taps),
		Cellar:     &cellar.Cellar{Path: paths.Cellar},
		Caskroom:   &cask.Caskroom{Path: paths.Caskroom},
	}, nil
}

// NewLoader creates a formula.Loader with debug logging wired in.
func NewLoader(tapDir string) *formula.Loader {
	l := formula.NewLoader(tapDir)
	l.DebugLog = func(format string, args ...any) {
		slog.Debug(fmt.Sprintf(format, args...))
	}
	return l
}

// NewCaskLoader creates a cask.Loader with debug logging wired in.
func NewCaskLoader(tapDir string) *cask.Loader {
	l := &cask.Loader{TapDir: tapDir}
	l.DebugLog = func(format string, args ...any) {
		slog.Debug(fmt.Sprintf(format, args...))
	}
	return l
}
