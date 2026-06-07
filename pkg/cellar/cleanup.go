package cellar

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/safepath"
)

type CleanupOpts struct {
	DryRun bool
	Scrub  bool
	Prune  string
}

type CleanupPaths struct {
	DownloadsDir string
	PruneDirs    []string
}

func (c *Cellar) RunCleanup(targets []string, opts CleanupOpts, paths CleanupPaths) (int64, error) {
	slog.Debug("starting cleanup execution in cellar")

	var totalBytes int64

	downloadsRoot := ""
	if paths.DownloadsDir != "" {
		if abs, err := filepath.Abs(paths.DownloadsDir); err == nil {
			downloadsRoot = filepath.Clean(abs)
		} else {
			downloadsRoot = filepath.Clean(paths.DownloadsDir)
		}

		if err := safepath.SafeAbsolutePath(downloadsRoot); err != nil {
			slog.Warn(fmt.Sprintf("skipping cleanup for invalid downloads dir %q: %v", downloadsRoot, err))
			downloadsRoot = ""
		}
	}

	// 1. Get full list of installed packages to know what is current.
	allInstalled, err := c.List()
	if err != nil {
		return 0, err
	}

	// 2. Determine which formulas we are cleaning up.
	cleanupTargets := allInstalled
	if len(targets) > 0 {
		targetSet := make(map[string]bool, len(targets))
		for _, t := range targets {
			targetSet[t] = true
		}
		var filtered []InstalledPackage
		for _, pkg := range allInstalled {
			if targetSet[pkg.Name] {
				filtered = append(filtered, pkg)
			}
		}
		cleanupTargets = filtered
	}

	// 3. Clean old version kegs in the Cellar.
	for _, pkg := range cleanupTargets {
		versions, err := c.InstalledVersions(pkg.Name)
		if err != nil || len(versions) <= 1 {
			continue
		}
		// Keep the latest (last after sort), remove the rest.
		for _, ver := range versions[:len(versions)-1] {
			kegPath, err := c.KegPath(pkg.Name, ver)
			if err != nil {
				continue
			}
			size, _ := fsutil.DirSize(kegPath)
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

	// 4. Determine max cache age.
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
			return totalBytes, fmt.Errorf("invalid prune value: %s", opts.Prune)
		}
		maxAge = time.Duration(days) * 24 * time.Hour
	}

	// 5. Clean download cache (Cache directory).
	if downloadsRoot != "" {
		if _, err := os.Stat(downloadsRoot); err == nil {
			tmpEntries, err := os.ReadDir(downloadsRoot)
			if err == nil {
				for _, e := range tmpEntries {
					name := e.Name()
					path, err := safepath.SafeJoin(downloadsRoot, name)
					if err != nil {
						slog.Warn(fmt.Sprintf("skipping cache entry outside downloads dir: %q", name))
						continue
					}

					// If specific targets provided, only clean files that seem to belong to them.
					if len(targets) > 0 && !BelongsToTargets(targets, name) {
						continue
					}

					info, err := e.Info()
					if err != nil {
						continue
					}

					tooOld := time.Since(info.ModTime()) > maxAge
					isLatest := IsLatestInstalled(allInstalled, name)

					// Remove if:
					// - Explicitly scrubbing
					// - Or it's older than the prune threshold
					// - Or it's NOT the latest installed version of a formula
					shouldRemove := opts.Scrub || tooOld || !isLatest

					if !shouldRemove {
						continue
					}

					size, _ := fsutil.EntrySize(path, e)
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
	}

	// 6. Prune empty directories in the prefix.
	if !opts.DryRun {
		for _, d := range paths.PruneDirs {
			fsutil.PruneEmptyDirs(d)
		}
		if downloadsRoot != "" {
			fsutil.PruneEmptyDirs(downloadsRoot)
		}
	}

	return totalBytes, nil
}

// BelongsToTargets reports whether the filename seems to belong to any of the target formula names.
func BelongsToTargets(targets []string, filename string) bool {
	for _, t := range targets {
		if strings.HasPrefix(filename, t+"-") {
			return true
		}
	}
	return false
}

// IsLatestInstalled reports whether a cache filename corresponds to a currently
// installed latest version of a formula.
func IsLatestInstalled(installed []InstalledPackage, filename string) bool {
	// Cache filenames are generally of the form:
	//   formula-version.ext (bottle)
	//   formula-version-src.ext (source)
	//   formula-version.sh (unshare script, though these shouldn't persist)
	//   grew-sandbox-*.sh (temporary scripts)

	for _, pkg := range installed {
		// pkg.Version is the latest installed version (from cel.List()).
		prefix := pkg.Name + "-" + pkg.Version
		if strings.HasPrefix(filename, prefix) {
			return true
		}
	}
	return false
}
