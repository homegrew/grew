package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cache"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/context"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/pkg/safepath"
)

// ReadContext bundles objects needed by read-only commands (info, search, outdated, deps).
type ReadContext = *context.Context

// NewReadContext initialises paths and the core tap for read-only commands.
func NewReadContext() (ReadContext, error) {
	return context.New()
}

// InstallContext bundles the common objects used by install, reinstall, and upgrade.
type InstallContext struct {
	ReadContext
	Linker     *linker.Linker
	DL         *downloader.Downloader
	AuditLog   *auditlog.Logger
	GlobalLock *os.File
}

func (c *InstallContext) Close() {
	if c.GlobalLock != nil {
		fsutil.Unlock(c.GlobalLock)
		if err := c.GlobalLock.Close(); err != nil {
			slog.Warn("close global lock", "error", err)
		}
		c.GlobalLock = nil
	}
}

// NewInstallContext initialises paths, the core tap, and returns the shared context.
func NewInstallContext() (*InstallContext, error) {
	common, err := context.New()
	if err != nil {
		return nil, err
	}

	if err := safepath.SafeAbsolutePath(common.Paths.Tmp); err != nil {
		return nil, fmt.Errorf("invalid temporary directory %q: %w", common.Paths.Tmp, err)
	}

	lock, err := acquireGlobalLock(common.Paths)
	if err != nil {
		return nil, err
	}

	return &InstallContext{
		ReadContext: common,
		Linker:      &linker.Linker{Paths: common.Paths},
		DL:          &downloader.Downloader{TmpDir: common.Paths.Tmp, Cache: cache.New(common.Paths.Cache)},
		AuditLog:    auditlog.New(common.Paths.Log),
		GlobalLock:  lock,
	}, nil
}

func acquireGlobalLock(paths config.Paths) (*os.File, error) {
	if err := safepath.SafeAbsolutePath(paths.Root); err != nil {
		return nil, fmt.Errorf("invalid root directory %q: %w", paths.Root, err)
	}
	rootAbs, err := filepath.Abs(paths.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)

	lockPath := filepath.Join(rootAbs, ".grew.lock")
	lockAbs, err := filepath.Abs(lockPath)
	if err != nil {
		return nil, fmt.Errorf("resolve lock file path: %w", err)
	}
	lockAbs = filepath.Clean(lockAbs)

	rel, err := filepath.Rel(rootAbs, lockAbs)
	if err != nil {
		return nil, fmt.Errorf("validate lock file path: %w", err)
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid lock file path %q: escapes root %q", lockAbs, rootAbs)
	}

	f, err := os.OpenFile(lockAbs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := fsutil.Lock(f); err != nil {
		if cerr := f.Close(); cerr != nil {
			return nil, fmt.Errorf("acquire global lock: %w (also failed to close lock file: %v)", err, cerr)
		}
		return nil, fmt.Errorf("acquire global lock: %w", err)
	}
	return f, nil
}
