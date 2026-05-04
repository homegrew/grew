package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cache"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
)

func NormalizeDir(dir, name string) (string, error) {
	cleanDir := dir
	if abs, err := filepath.Abs(cleanDir); err == nil {
		cleanDir = abs
	}
	if eval, err := filepath.EvalSymlinks(cleanDir); err == nil {
		cleanDir = eval
	}
	cleanDir = filepath.Clean(cleanDir)
	if err := safepath.SafeAbsolutePath(cleanDir); err != nil {
		return "", fmt.Errorf("invalid %s directory %q: %w", name, cleanDir, err)
	}
	return cleanDir, nil
}

func GatherDeps(loader *formula.Loader, deps []string, seen map[string]bool, includeBuild bool) error {
	for _, dep := range deps {
		if seen[dep] {
			continue
		}
		seen[dep] = true
		f, err := loader.LoadByName(dep)
		if err != nil {
			return fmt.Errorf("dependency %q not found", dep)
		}
		subDeps := f.Dependencies
		if includeBuild {
			subDeps = append(subDeps, f.BuildDependencies...)
		}
		if err := GatherDeps(loader, subDeps, seen, includeBuild); err != nil {
			return err
		}
	}
	return nil
}

func UninstallFormula(ctx *InstallContext, name string, force bool) error {
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
	ctx.Linker.Unlink(name)
	slog.Info("removed symlinks from bin/, lib/, include/, opt/")

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

type CleanupOpts struct {
	DryRun bool
	Scrub  bool
	Prune  string
}

func RunCleanup(args []string, opts CleanupOpts) error {
	slog.Debug("starting cleanup command execution")

	targets := args
	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	var totalBytes int64

	allInstalled, err := cel.List()
	if err != nil {
		return err
	}

	cleanupTargets := allInstalled
	if len(targets) > 0 {
		targetSet := make(map[string]bool, len(targets))
		for _, t := range targets {
			targetSet[t] = true
		}
		var filtered []cellar.InstalledPackage
		for _, pkg := range allInstalled {
			if targetSet[pkg.Name] {
				filtered = append(filtered, pkg)
			}
		}
		cleanupTargets = filtered
	}

	for _, pkg := range cleanupTargets {
		versions, err := cel.InstalledVersions(pkg.Name)
		if err != nil || len(versions) <= 1 {
			continue
		}
		for _, ver := range versions[:len(versions)-1] {
			kegPath, err := cel.KegPath(pkg.Name, ver)
			if err != nil {
				continue
			}
			size, _ := DirSize(kegPath)
			totalBytes += size
			if opts.DryRun {
				fmt.Printf("Would remove: %s %s (%s)\n", pkg.Name, ver, fsutil.FormatSize(size))
			} else {
				slog.Debug(fmt.Sprintf("removing old keg %s/%s", pkg.Name, ver))
				if err := os.RemoveAll(kegPath); err != nil {
					slog.Warn(fmt.Sprintf("could not remove %s: %v", kegPath, err))
				} else {
					fmt.Printf("Removing: %s %s (%s)\n", pkg.Name, ver, fsutil.FormatSize(size))
				}
			}
		}
	}

	maxAgeDays := 120
	if env := os.Getenv("HOMEGREW_CLEANUP_MAX_AGE_DAYS"); env != "" {
		if d, err := strconv.Atoi(env); err == nil {
			maxAgeDays = d
		}
	}
	maxAge := time.Duration(maxAgeDays) * 24 * time.Hour

	if opts.Prune == "all" {
		maxAge = 0
	} else if opts.Prune != "" {
		days, err := strconv.Atoi(opts.Prune)
		if err != nil {
			return fmt.Errorf("invalid prune value: %s", opts.Prune)
		}
		maxAge = time.Duration(days) * 24 * time.Hour
	}

	downloadsDir := cache.New(paths.Cache).DownloadsDir()
	if _, err := os.Stat(downloadsDir); err == nil {
		tmpEntries, err := os.ReadDir(downloadsDir)
		if err == nil {
			for _, e := range tmpEntries {
				name := e.Name()
				path := filepath.Join(downloadsDir, name)

				if len(targets) > 0 && !BelongsToTargets(targets, name) {
					continue
				}

				info, err := e.Info()
				if err != nil {
					continue
				}

				tooOld := time.Since(info.ModTime()) > maxAge
				isLatest := IsLatestInstalled(allInstalled, name)

				shouldRemove := opts.Scrub || tooOld || !isLatest

				if !shouldRemove {
					continue
				}

				size, _ := EntrySize(path, e)
				totalBytes += size
				if opts.DryRun {
					fmt.Printf("Would remove: %s (%s)\n", path, fsutil.FormatSize(size))
				} else {
					slog.Debug("removing cached file " + name)
					if err := os.RemoveAll(path); err != nil {
						slog.Warn(fmt.Sprintf("could not remove %s: %v", path, err))
					} else {
						fmt.Printf("Removing: %s (%s)\n", path, fsutil.FormatSize(size))
					}
				}
			}
		}
	}

	if !opts.DryRun {
		PruneEmptyDirs(paths.Bin)
		PruneEmptyDirs(paths.Lib)
		PruneEmptyDirs(paths.Include)
		PruneEmptyDirs(paths.Share)
		PruneEmptyDirs(downloadsDir)
	}

	if totalBytes == 0 {
		fmt.Println("Already clean, nothing to do.")
	} else if opts.DryRun {
		ui.FprintArrow(os.Stderr, "Would free %s", fsutil.FormatSize(totalBytes))
	} else {
		ui.FprintArrow(os.Stderr, "Freed %s", fsutil.FormatSize(totalBytes))
	}

	return nil
}

func BelongsToTargets(targets []string, filename string) bool {
	for _, t := range targets {
		if strings.HasPrefix(filename, t+"-") {
			return true
		}
	}
	return false
}

func IsLatestInstalled(installed []cellar.InstalledPackage, filename string) bool {
	for _, pkg := range installed {
		prefix := pkg.Name + "-" + pkg.Version
		if strings.HasPrefix(filename, prefix) {
			return true
		}
	}
	return false
}

func PruneEmptyDirs(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() || path == root {
			return nil
		}
		PruneEmptyDirs(path)
		entries, err := os.ReadDir(path)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(path)
		}
		return filepath.SkipDir
	})
}

func DirSize(path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func EntrySize(path string, e os.DirEntry) (int64, error) {
	info, err := e.Info()
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return DirSize(path)
	}
	return info.Size(), nil
}
