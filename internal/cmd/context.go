package cmd

import (
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/tap"
)

// commonCtx bundles objects used by most commands.
type commonCtx struct {
	Paths      config.Paths
	Loader     *formula.Loader
	CaskLoader *cask.Loader
	Cellar     *cellar.Cellar
}

// newCommonCtx initialises paths and the core tap, returning a shared context.
func newCommonCtx() (*commonCtx, error) {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return nil, err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		return nil, fmt.Errorf("init core tap: %w", err)
	}

	return &commonCtx{
		Paths:      paths,
		Loader:     newLoader(paths.Taps),
		CaskLoader: newCaskLoader(paths.Taps),
		Cellar:     &cellar.Cellar{Path: paths.Cellar},
	}, nil
}

// newLoader creates a formula.Loader with debug logging wired in.
func newLoader(tapDir string) *formula.Loader {
	l := formula.NewLoader(tapDir)
	l.DebugLog = func(format string, args ...any) {
		slog.Debug(fmt.Sprintf(format, args...))
	}
	return l
}
