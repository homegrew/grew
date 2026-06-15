package context

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/auditlog"
	"github.com/homegrew/grew/pkg/cache"
	"github.com/homegrew/grew/pkg/cask"
	"github.com/homegrew/grew/pkg/cellar"
	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/downloader"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/homebrew"
	"github.com/homegrew/grew/pkg/linker"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/tap"
	"github.com/homegrew/grew/pkg/ui"
)

// Context bundles objects used by most commands.
type Context struct {
	// Paths provides access to all major directories used by grew.
	Paths config.Paths
	// Loader is used to load formulas.
	Loader *formula.Loader
	// CaskLoader is used to load casks.
	CaskLoader *cask.Loader
	// Cellar provides access to the directory where formulas are installed.
	Cellar *cellar.Cellar
	// Caskroom provides access to the directory where casks are recorded.
	Caskroom *cask.Caskroom
}

// New initialises the default grew paths and, unless HOMEGREW_NO_INIT_TAP is
// set, initialises the core tap, then returns a shared Context configured with
// loaders, cellar, and caskroom paths.
//
// It returns an error if path initialisation fails, or if core tap
// initialisation fails when tap initialisation is enabled.
func New() (*Context, error) {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return nil, err
	}

	if os.Getenv("HOMEGREW_NO_INIT_TAP") == "" {
		tapMgr := &tap.Manager{TapsDir: paths.Taps}
		if err := tapMgr.InitCore(); err != nil {
			return nil, fmt.Errorf("init core tap: %w", err)
		}
	}

	return &Context{
		Paths:      paths,
		Loader:     NewLoader(paths.Taps),
		CaskLoader: NewCaskLoader(paths.Taps),
		Cellar:     &cellar.Cellar{Path: paths.Cellar},
		Caskroom:   &cask.Caskroom{Path: paths.Caskroom},
	}, nil
}

// loadWithAutotap loads a package by name using the provided local loader,
// auto-tapping a fully qualified tap on demand and falling back to the remote
// loader (Homebrew API) when the local load fails. On total failure it returns
// the original local error.
func loadWithAutotap[T any](ctx *Context, kind, name string, local, remote func(string) (T, error)) (T, error) {
	v, err := local(name)
	if err != nil && strings.Contains(name, "/") {
		// Attempt to auto-tap if it's a fully qualified name
		parts := strings.Split(name, "/")
		tapName := parts[0] + "/" + parts[1]
		ui.FprintArrow(os.Stderr, "%s not found. Auto-tapping %s...", kind, tapName)
		mgr := &tap.Manager{TapsDir: ctx.Paths.Taps}
		if tapErr := mgr.Add(tapName, ""); tapErr == nil {
			v, err = local(name)
		}
	}
	if err == nil {
		return v, nil
	}

	// Fallback to Homebrew API
	slog.Debug(strings.ToLower(kind)+" not found locally, trying Homebrew API", "name", name)
	if remoteV, remoteErr := remote(name); remoteErr == nil {
		return remoteV, nil
	}

	return v, err // Return original error if remote also fails
}

// LoadFormula attempts to load a formula by name from local taps, falling back
// to the Homebrew API if not found.
func (ctx *Context) LoadFormula(name string) (*formula.Formula, error) {
	return loadWithAutotap(ctx, "Formula", name, ctx.Loader.LoadByName, homebrew.FetchFormula)
}

// LoadCask attempts to load a cask by name from local taps, falling back
// to the Homebrew API if not found.
func (ctx *Context) LoadCask(name string) (*cask.Cask, error) {
	return loadWithAutotap(ctx, "Cask", name, ctx.CaskLoader.LoadByName, homebrew.FetchCask)
}

// ResolveKind determines whether name should be treated as a cask or a
// formula. When forceCask or forceFormula is set, the kind is fixed and the
// method only verifies that the package exists in the corresponding local tap.
// In auto mode (neither forced), a formula takes precedence over a cask of the
// same name; if neither exists locally an error is returned.
func (ctx *Context) ResolveKind(name string, forceCask, forceFormula bool) (isCask bool, err error) {
	switch {
	case forceCask:
		if _, err := ctx.CaskLoader.LoadByName(name); err != nil {
			return true, err
		}
		return true, nil
	case forceFormula:
		if _, err := ctx.Loader.LoadByName(name); err != nil {
			return false, err
		}
		return false, nil
	default:
		if _, err := ctx.Loader.LoadByName(name); err == nil {
			return false, nil
		}
		if _, err := ctx.CaskLoader.LoadByName(name); err == nil {
			return true, nil
		}
		return false, fmt.Errorf("no formula or cask found for %q", name)
	}
}

// slogDebugLog adapts a printf-style debug logger to slog.Debug. Shared by the
// formula and cask loader constructors.
var slogDebugLog = func(format string, args ...any) {
	slog.Debug(fmt.Sprintf(format, args...))
}

// NewLoader creates a formula.Loader with debug logging wired in.
func NewLoader(tapDir string) *formula.Loader {
	l := formula.NewLoader(tapDir)
	l.DebugLog = slogDebugLog
	return l
}

// NewCaskLoader creates a cask.Loader with debug logging wired in.
func NewCaskLoader(tapDir string) *cask.Loader {
	l := &cask.Loader{TapDir: tapDir}
	l.DebugLog = slogDebugLog
	return l
}

// InstallContext bundles the common objects used by install, reinstall, and upgrade.
type InstallContext struct {
	// Context is the common context used by most commands.
	*Context
	// Linker is used to manage symlinks for installed formulas.
	Linker *linker.Linker
	// DL is used to download files.
	DL *downloader.Downloader
	// AuditLog is used to log installation and uninstallation actions.
	AuditLog *auditlog.Logger
	// GlobalLock is the file handle for the global lock.
	GlobalLock *os.File
}

// Close releases resources held by the InstallContext, such as the global lock.
func (c *InstallContext) Close() {
	if c.GlobalLock != nil {
		fsutil.Unlock(c.GlobalLock)
		if err := c.GlobalLock.Close(); err != nil {
			slog.Warn("close global lock", "error", err)
		}
		c.GlobalLock = nil
	}
}

// UninstallFormula uninstalls a formula, unlinking it first.
func (ctx *InstallContext) UninstallFormula(name string, force bool) error {
	if !ctx.Cellar.IsInstalled(name) {
		if !force {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
		}
		return nil
	}

	ver, _ := ctx.Cellar.InstalledVersion(name)
	kegPath, _ := ctx.Cellar.KegPath(name, ver)
	slog.Info("cellar path: " + kegPath)

	ui.FprintArrow(os.Stderr, "Unlinking %s...", name)
	if err := ctx.Linker.Unlink(name); err != nil {
		if force {
			slog.Warn(fmt.Sprintf("ignoring error while unlinking %s: %v", name, err))
		} else {
			return err
		}
	} else {
		slog.Info("removed symlinks from bin/, lib/, include/, opt/")
	}

	ui.FprintArrow(os.Stderr, "Removing %s...", name)
	if err := ctx.Cellar.Uninstall(name); err != nil {
		if force {
			slog.Warn(fmt.Sprintf("ignoring error while removing %s: %v", name, err))
		} else {
			return err
		}
	}

	ctx.AuditLog.Log(auditlog.ActionUninstall, name, ver, "", "")
	ui.FprintArrow(os.Stderr, "%s uninstalled", name)
	return nil
}

// NewInstallContext initialises paths, the core tap, and returns the shared context for destructive operations.
func NewInstallContext() (*InstallContext, error) {
	common, err := New()
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
		Context:    common,
		Linker:     &linker.Linker{Paths: common.Paths},
		DL:         &downloader.Downloader{TmpDir: common.Paths.Tmp, Cache: cache.New(common.Paths.Cache)},
		AuditLog:   auditlog.New(common.Paths.Log),
		GlobalLock: lock,
	}, nil
}

func canonicalPath(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func acquireGlobalLock(paths config.Paths) (*os.File, error) {
	if err := safepath.SafeAbsolutePath(paths.Root); err != nil {
		return nil, fmt.Errorf("invalid root directory %q: %w", paths.Root, err)
	}
	if err := safepath.SafeAbsolutePath(paths.Locks); err != nil {
		return nil, fmt.Errorf("invalid locks directory %q: %w", paths.Locks, err)
	}
	if err := safepath.CheckSubpath(paths.Root, paths.Locks); err != nil {
		return nil, fmt.Errorf("invalid locks directory %q: %w", paths.Locks, err)
	}

	rootCanon, err := canonicalPath(paths.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory %q: %w", paths.Root, err)
	}
	if err := safepath.SafeAbsolutePath(rootCanon); err != nil {
		return nil, fmt.Errorf("invalid root directory %q: %w", rootCanon, err)
	}

	locksCanon, err := safepath.SafeJoin(rootCanon, "var", "homegrew", "locks")
	if err != nil {
		return nil, fmt.Errorf("invalid locks directory under root %q: %w", rootCanon, err)
	}
	if err := safepath.SafeAbsolutePath(locksCanon); err != nil {
		return nil, fmt.Errorf("invalid locks directory %q: %w", locksCanon, err)
	}
	if err := safepath.CheckSubpath(rootCanon, locksCanon); err != nil {
		return nil, fmt.Errorf("invalid locks directory %q: %w", locksCanon, err)
	}

	locksInfo, err := os.Lstat(locksCanon)
	if err != nil {
		return nil, fmt.Errorf("stat locks directory %q: %w", locksCanon, err)
	}
	if locksInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid locks directory %q: must not be a symlink", locksCanon)
	}
	if !locksInfo.IsDir() {
		return nil, fmt.Errorf("invalid locks directory %q: not a directory", locksCanon)
	}
	lockAbs, err := safepath.SafeJoin(locksCanon, ".grew.lock")
	if err != nil {
		return nil, fmt.Errorf("invalid lock file path: %w", err)
	}
	if err := safepath.CheckSubpath(locksCanon, lockAbs); err != nil {
		return nil, fmt.Errorf("invalid lock file path %q: %w", lockAbs, err)
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
