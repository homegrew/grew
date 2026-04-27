package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/tap"
	"github.com/homegrew/grew/pkg/logger"
	"github.com/homegrew/grew/pkg/safepath"
	"strings"
)

func newCaskLoader(tapDir string) *cask.Loader {
	l := &cask.Loader{TapDir: tapDir}
	l.DebugLog = func(format string, args ...any) {
		slog.Debug(fmt.Sprintf(format, args...))
	}
	return l
}

func initCaskTap(paths config.Paths) error {
	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	return tapMgr.InitCask()
}

func setupCaskLoader() (config.Paths, *cask.Loader, *cask.Caskroom, error) {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return config.Paths{}, nil, nil, err
	}
	if err := initCaskTap(paths); err != nil {
		return config.Paths{}, nil, nil, fmt.Errorf("init cask tap: %w", err)
	}
	loader := newCaskLoader(paths.Taps)
	cr := &cask.Caskroom{Path: paths.Caskroom}
	return paths, loader, cr, nil
}

func loadCask(name string) (config.Paths, *cask.Cask, *cask.Caskroom, error) {
	paths, loader, cr, err := setupCaskLoader()
	if err != nil {
		return paths, nil, nil, err
	}
	c, err := loader.LoadByName(name)
	if err != nil {
		return paths, nil, nil, fmt.Errorf("cask not found: %s", name)
	}
	return paths, c, cr, nil
}

// removeIfWithin deletes targetPath only if it resolves within baseDir.
// If the check fails, it returns an error and does not attempt deletion.
func removeIfWithin(targetPath, baseDir string) error {
	if targetPath == "" || baseDir == "" {
		return fmt.Errorf("empty path for removal")
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolve base dir: %w", err)
	}
	canonBase, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return fmt.Errorf("resolve base symlinks: %w", err)
	}
	canonBase = filepath.Clean(canonBase)
	if err := safepath.SafeAbsolutePath(canonBase); err != nil {
		return fmt.Errorf("invalid canonical base path %q: %w", canonBase, err)
	}

	targetAbs, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}
	targetAbs = filepath.Clean(targetAbs)

	targetBase := filepath.Base(targetAbs)
	if err := safepath.SafePathComponent(targetBase); err != nil {
		return fmt.Errorf("invalid target filename %q: %w", targetBase, err)
	}

	targetParent := filepath.Dir(targetAbs)
	canonParent, err := filepath.EvalSymlinks(targetParent)
	if err != nil {
		return fmt.Errorf("resolve target parent symlinks: %w", err)
	}
	canonParent = filepath.Clean(canonParent)
	if err := safepath.SafeAbsolutePath(canonParent); err != nil {
		return fmt.Errorf("invalid canonical target parent path %q: %w", canonParent, err)
	}
	if err := safepath.CheckSubpath(canonBase, canonParent); err != nil {
		return fmt.Errorf("refusing to remove path outside base directory: %s", targetAbs)
	}

	canonTarget, err := safepath.SafeJoin(canonParent, targetBase)
	if err != nil {
		return fmt.Errorf("resolve canonical target path: %w", err)
	}
	if err := safepath.SafeAbsolutePath(canonTarget); err != nil {
		return fmt.Errorf("invalid canonical target path %q: %w", canonTarget, err)
	}

	if canonTarget == canonBase {
		return fmt.Errorf("refusing to remove base directory path: %s", canonTarget)
	}
	if err := safepath.CheckSubpath(canonBase, canonTarget); err != nil {
		return fmt.Errorf("refusing to remove path outside base directory: %s", canonTarget)
	}

	fi, err := os.Lstat(canonTarget)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat target for removal: %w", err)
	}
	if fi.IsDir() {
		return fmt.Errorf("refusing to remove directory path: %s", canonTarget)
	}

	// Sink-adjacent hardening: rebuild the final remove target from the
	// validated canonical parent and a validated single path component.
	baseName := filepath.Base(canonTarget)
	if err := safepath.SafePathComponent(baseName); err != nil {
		return fmt.Errorf("invalid target filename for removal %q: %w", baseName, err)
	}
	safeTarget, err := safepath.SafeJoin(canonParent, baseName)
	if err != nil {
		return fmt.Errorf("resolve safe target for removal: %w", err)
	}
	if safeTarget != canonTarget {
		return fmt.Errorf("refusing to remove non-canonical target path: %s", canonTarget)
	}

	return os.Remove(safeTarget)
}

func caskInstall(name string, noQuarantine bool, force bool) (err error) {
	paths, c, cr, err := loadCask(name)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			slog.Error("cask installation failed, cleaning up", "cask", c.Name, "error", err)
			// Best-effort cleanup of installed artifacts.
			// We don't have a surgical way to know which ones were partially installed,
			// so we use caskUninstall which is safe even if some parts are missing.
			_ = caskUninstall(c.Name, true)
		}
	}()

	if cr.IsInstalled(c.Name) && !force {
		fmt.Fprintf(os.Stderr, "==> %s %s is already installed, skipping\n", c.Name, c.Version)
		return nil
	}

	// Validate cask-derived identifiers before using them in any filesystem paths.
	if err := safepath.SafePathComponent(c.Name); err != nil {
		return fmt.Errorf("invalid cask name: %w", err)
	}
	if err := safepath.SafePathComponent(c.Version); err != nil {
		return fmt.Errorf("invalid cask version: %w", err)
	}

	defer logger.TimeOp(fmt.Sprintf("install cask %s %s", c.Name, c.Version))()
	slog.Debug("platform: " + formula.PlatformKey())
	fmt.Fprintf(os.Stderr, "==> Installing cask %s %s\n", c.Name, c.Version)

	dlURL, err := c.GetURL()
	if err != nil {
		return err
	}
	slog.Info("URL: " + dlURL)

	sha, err := c.GetSHA256()
	if err != nil {
		return err
	}
	slog.Info("expected SHA256: " + sha)

	sha512 := c.GetSHA512()
	if sha512 != "" {
		slog.Info("expected SHA512: " + sha512)
	}

	dl := &downloader.Downloader{TmpDir: paths.Tmp, CacheDir: paths.Cache}
	filename := c.Name + "-" + c.Version + caskURLExt(dlURL)
	// Ensure the constructed filename is a single safe path component.
	if err := safepath.SafePathComponent(filename); err != nil {
		return fmt.Errorf("invalid download filename: %w", err)
	}
	localFile, err := dl.Download(dlURL, filename)
	if err != nil {
		return fmt.Errorf("download %s: %w", c.Name, err)
	}

	// Re-canonicalize and constrain downloader output before any sink usage.
	tmpAbs, err := filepath.Abs(paths.Tmp)
	if err != nil {
		return fmt.Errorf("resolve temp directory: %w", err)
	}
	canonTmp, err := filepath.EvalSymlinks(tmpAbs)
	if err != nil {
		return fmt.Errorf("resolve temp directory symlinks: %w", err)
	}
	canonTmp = filepath.Clean(canonTmp)
	if err := safepath.SafeAbsolutePath(canonTmp); err != nil {
		return fmt.Errorf("invalid temp directory path %q: %w", canonTmp, err)
	}

	localAbs, err := filepath.Abs(localFile)
	if err != nil {
		return fmt.Errorf("resolve downloaded path: %w", err)
	}
	canonLocal, err := filepath.EvalSymlinks(localAbs)
	if err != nil {
		if os.IsNotExist(err) {
			canonLocal = filepath.Clean(localAbs)
		} else {
			return fmt.Errorf("resolve downloaded path symlinks: %w", err)
		}
	} else {
		canonLocal = filepath.Clean(canonLocal)
	}
	if err := safepath.SafeAbsolutePath(canonLocal); err != nil {
		return fmt.Errorf("invalid downloaded path %q: %w", canonLocal, err)
	}
	if err := safepath.CheckSubpath(canonTmp, canonLocal); err != nil {
		return fmt.Errorf("downloaded path escapes temp directory: %w", err)
	}
	localFile = canonLocal

	slog.Info("saved to: " + localFile)

	if err := downloader.VerifySHA256Within(paths.Tmp, localFile, sha); err != nil {
		// Best-effort cleanup of the downloaded file, constrained to the temp directory.
		_ = removeIfWithin(localFile, paths.Tmp)
		return fmt.Errorf("verify %s: %w", c.Name, err)
	}
	fmt.Fprintf(os.Stderr, "==> SHA256 verified\n")

	if sha512 != "" {
		if err := downloader.VerifySHA512Within(paths.Tmp, localFile, sha512); err != nil {
			_ = removeIfWithin(localFile, paths.Tmp)
			return fmt.Errorf("verify %s (SHA512): %w", c.Name, err)
		}
		fmt.Fprintf(os.Stderr, "==> SHA512 verified\n")
	}

	// Extract archive to staging
	if err := safepath.SafePathComponent(c.Name); err != nil {
		_ = removeIfWithin(localFile, paths.Tmp)
		return fmt.Errorf("invalid cask name for staging directory: %w", err)
	}
	if err := safepath.SafePathComponent(c.Version); err != nil {
		_ = removeIfWithin(localFile, paths.Tmp)
		return fmt.Errorf("invalid cask version for staging directory: %w", err)
	}
	// Build a staging directory inside the configured temporary directory,
	// and ensure it does not escape that base.
	stageName := c.Name + "-" + c.Version + "-cask-stage"
	if err := safepath.SafePathComponent(stageName); err != nil {
		return fmt.Errorf("invalid staging directory name: %w", err)
	}
	stageDir, err := safepath.SafeJoin(paths.Tmp, stageName)
	if err != nil {
		os.Remove(localFile)
		return fmt.Errorf("staging directory escapes tmp directory: %w", err)
	}
	os.RemoveAll(stageDir)

	spec := formula.InstallSpec{Type: "archive", StripComponents: 0}
	if err := downloader.Extract(localFile, stageDir, spec); err != nil {
		os.RemoveAll(stageDir)
		_ = removeIfWithin(localFile, paths.Tmp)
		return fmt.Errorf("extract %s: %w", c.Name, err)
	}
	slog.Info("extracted to staging: " + stageDir)

	inst := &cask.Installer{AppDir: paths.AppDir, BinDir: paths.Bin}

	// Install .app artifacts
	for _, appName := range c.Artifacts.App {
		dest, err := inst.InstallApp(stageDir, appName)
		if err != nil {
			os.RemoveAll(stageDir)
			_ = removeIfWithin(localFile, paths.Tmp)
			return fmt.Errorf("install artifact %s: %w", appName, err)
		}
		if noQuarantine {
			slog.Info("quarantine skipped (--no-quarantine)")
		} else {
			if err := applyCaskQuarantine(dest); err != nil {
				// Roll back: remove the app we just installed.
				os.RemoveAll(dest)
				os.RemoveAll(stageDir)
				_ = removeIfWithin(localFile, paths.Tmp)
				return err
			}
			slog.Info("quarantine attribute set")
		}
		fmt.Fprintf(os.Stderr, "==> Installed %s to %s\n", appName, dest)
	}

	// Link bin artifacts
	for _, binName := range c.Artifacts.Bin {
		// Look for binary inside the .app bundle or staging dir
		binTarget := findCaskBinary(paths.AppDir, c.Artifacts.App, binName)
		if binTarget != "" {
			if err := inst.LinkBin(binName, binTarget); err != nil {
				slog.Warn(fmt.Sprintf("could not link binary %s: %v", binName, err))
			} else {
				slog.Info(fmt.Sprintf("linked binary: %s -> %s", binName, binTarget))
			}
		}
	}

	// Record installation
	if err := cr.Record(c.Name, c.Version); err != nil {
		return fmt.Errorf("record cask installation: %w", err)
	}

	os.RemoveAll(stageDir)
	_ = removeIfWithin(localFile, paths.Tmp)

	fmt.Fprintf(os.Stderr, "==> %s %s installed\n", c.Name, c.Version)
	return nil
}

func caskUninstall(name string, force bool) error {
	paths, loader, cr, err := setupCaskLoader()
	if err != nil {
		return err
	}

	if !cr.IsInstalled(name) {
		if !force {
			return fmt.Errorf("cask %q is not installed", name)
		}
		return nil
	}

	c, err := loader.LoadByName(name)

	inst := &cask.Installer{AppDir: paths.AppDir, BinDir: paths.Bin}

	// Remove app artifacts
	if err == nil {
		for _, appName := range c.Artifacts.App {
			fmt.Fprintf(os.Stderr, "==> Removing %s...\n", appName)
			if err := inst.UninstallApp(appName); err != nil {
				if force {
					slog.Warn(fmt.Sprintf("ignoring error while removing %s: %v", appName, err))
				} else {
					slog.Warn(fmt.Sprintf("could not remove %s: %v", appName, err))
				}
			}
		}
		for _, binName := range c.Artifacts.Bin {
			if err := inst.UnlinkBin(binName); err != nil {
				if force {
					slog.Warn(fmt.Sprintf("ignoring error while unlinking binary %s: %v", binName, err))
				} else {
					slog.Warn(fmt.Sprintf("could not unlink binary %s: %v", binName, err))
				}
			}
		}
	}

	if err := cr.Remove(name); err != nil {
		if force {
			slog.Warn(fmt.Sprintf("ignoring error while removing cask %s from Caskroom: %v", name, err))
		} else {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "==> %s uninstalled\n", name)
	return nil
}

func caskList() error {
	paths := config.Default()
	cr := &cask.Caskroom{Path: paths.Caskroom}

	casks, err := cr.List()
	if err != nil {
		return err
	}
	if len(casks) == 0 {
		fmt.Println("No casks installed.")
		return nil
	}
	for _, c := range casks {
		fmt.Printf("%-20s %s\n", c.Name, c.Version)
	}
	return nil
}

func caskInfo(name string) error {
	_, c, cr, err := loadCask(name)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %s %s (cask)\n", c.Name, c.Description, c.Version)
	fmt.Printf("Homepage: %s\n", c.Homepage)
	fmt.Printf("License:  %s\n", c.License)

	if cr.IsInstalled(c.Name) {
		ver, _ := cr.InstalledVersion(c.Name)
		fmt.Printf("Installed: %s\n", ver)
	} else {
		fmt.Println("Installed: no")
	}

	if len(c.Artifacts.App) > 0 {
		fmt.Printf("Apps: %s\n", strings.Join(c.Artifacts.App, ", "))
	}
	if len(c.Artifacts.Bin) > 0 {
		fmt.Printf("Binaries: %s\n", strings.Join(c.Artifacts.Bin, ", "))
	}

	platforms := make([]string, 0, len(c.URL))
	for k := range c.URL {
		platforms = append(platforms, k)
	}
	fmt.Printf("Platforms: %s\n", strings.Join(platforms, ", "))

	return nil
}

func caskSearch(query string) error {
	_, loader, cr, err := setupCaskLoader()
	if err != nil {
		return err
	}

	all, err := loader.LoadAll()
	if err != nil {
		return err
	}

	found := false
	q := strings.ToLower(query)

	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Name), q) ||
			strings.Contains(strings.ToLower(c.Description), q) {
			marker := " "
			if cr.IsInstalled(c.Name) {
				marker = "*"
			}
			fmt.Printf("%s %-20s %s (cask)\n", marker, c.Name, c.Description)
			found = true
		}
	}

	if !found {
		fmt.Printf("No casks found matching %q\n", query)
	}
	return nil
}

// findCaskBinary looks for a binary inside a .app bundle's MacOS directory.
func findCaskBinary(appDir string, apps []string, binName string) string {
	if err := safepath.SafeAbsolutePath(appDir); err != nil {
		return ""
	}
	if err := safepath.SafePathComponent(binName); err != nil {
		return ""
	}
	for _, appName := range apps {
		if err := safepath.SafePathComponent(appName); err != nil {
			continue
		}
		candidate, err := safepath.SafeJoin(appDir, appName, "Contents", "MacOS", binName)
		if err == nil {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		// Also check Contents/Resources
		candidate, err = safepath.SafeJoin(appDir, appName, "Contents", "Resources", binName)
		if err == nil {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

func caskURLExt(rawURL string) string {
	if ext := urlExt(rawURL); ext != "" {
		return ext
	}
	return ".zip" // default for casks
}
