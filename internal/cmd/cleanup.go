package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/flags"
)

func runCleanup(args []string) error {
	slog.Debug("starting cleanup command execution")
	slog.Debug("starting cleanup command execution")
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew cleanup [options] [formula ...]

Remove old versions of installed formulas and clear old downloads from the cache.
If specific formulas are provided, only those formulas are cleaned up.

Options:
  -n, --dry-run   Show what would be removed, but do not actually remove anything.
  -s, --scrub     Remove all cached downloads, including those for the latest versions.
      --prune=DAYS Remove all cache files older than specified days.
  -v, --verbose   Show detailed output.
  -d, --debug     Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	dryRun := fs.Bool("dry-run", false, "Show what would be removed")
	fs.BoolVar(dryRun, "n", false, "Show what would be removed")
	scrub := fs.Bool("scrub", false, "Remove all cached downloads")
	fs.BoolVar(scrub, "s", false, "Remove all cached downloads")
	prune := fs.String("prune", "", "Remove all cache files older than specified days.")

	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	targets := fs.Args()

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	var totalBytes int64

	// 1. Get full list of installed packages to know what is current.
	allInstalled, err := cel.List()
	if err != nil {
		return err
	}

	// 2. Determine which formulas we are cleaning up.
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

	// 3. Clean old version kegs in the Cellar.
	for _, pkg := range cleanupTargets {
		versions, err := cel.InstalledVersions(pkg.Name)
		if err != nil || len(versions) <= 1 {
			continue
		}
		// Keep the latest (last after sort), remove the rest.
		for _, ver := range versions[:len(versions)-1] {
			kegPath, err := cel.KegPath(pkg.Name, ver)
			if err != nil {
				continue
			}
			size, _ := dirSize(kegPath)
			totalBytes += size
			if *dryRun {
				fmt.Printf("Would remove: %s %s (%s)\n", pkg.Name, ver, formatSize(size))
			} else {
				slog.Debug(fmt.Sprintf("removing old keg %s/%s", pkg.Name, ver))
				if err := os.RemoveAll(kegPath); err != nil {
					slog.Warn(fmt.Sprintf("could not remove %s: %v", kegPath, err))
				} else {
					fmt.Printf("Removing: %s %s (%s)\n", pkg.Name, ver, formatSize(size))
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

	if *prune == "all" {
		maxAge = 0
	} else if *prune != "" {
		days, err := strconv.Atoi(*prune)
		if err != nil {
			return fmt.Errorf("invalid prune value: %s", *prune)
		}
		maxAge = time.Duration(days) * 24 * time.Hour
	}

	// 5. Clean download cache (Cache directory).
	downloadsDir := filepath.Join(paths.Cache, "downloads")
	if _, err := os.Stat(downloadsDir); err == nil {
		tmpEntries, err := os.ReadDir(downloadsDir)
		if err == nil {
			for _, e := range tmpEntries {
				name := e.Name()
				path := filepath.Join(downloadsDir, name)

				// If specific targets provided, only clean files that seem to belong to them.
				if len(targets) > 0 && !belongsToTargets(targets, name) {
					continue
				}

				info, err := e.Info()
				if err != nil {
					continue
				}

				tooOld := time.Since(info.ModTime()) > maxAge
				isLatest := isLatestInstalled(allInstalled, name)

				// Remove if:
				// - Explicitly scrubbing
				// - Or it's older than the prune threshold
				// - Or it's NOT the latest installed version of a formula
				shouldRemove := *scrub || tooOld || !isLatest

				if !shouldRemove {
					continue
				}

				size, _ := entrySize(path, e)
				totalBytes += size
				if *dryRun {
					fmt.Printf("Would remove: %s (%s)\n", path, formatSize(size))
				} else {
					slog.Debug("removing cached file " + name)
					if err := os.RemoveAll(path); err != nil {
						slog.Warn(fmt.Sprintf("could not remove %s: %v", path, err))
					} else {
						fmt.Printf("Removing: %s (%s)\n", path, formatSize(size))
					}
				}
			}
		}
	}

	// 6. Prune empty directories in the prefix.
	if !*dryRun {
		pruneEmptyDirs(paths.Bin)
		pruneEmptyDirs(paths.Lib)
		pruneEmptyDirs(paths.Include)
		pruneEmptyDirs(paths.Share)
		pruneEmptyDirs(downloadsDir)
	}

	if totalBytes == 0 {
		fmt.Println("Already clean, nothing to do.")
	} else if *dryRun {
		fmt.Fprintf(os.Stderr, "==> Would free %s\n", formatSize(totalBytes))
	} else {
		fmt.Fprintf(os.Stderr, "==> Freed %s\n", formatSize(totalBytes))
	}

	return nil
}

// belongsToTargets reports whether the filename seems to belong to any of the target formula names.
func belongsToTargets(targets []string, filename string) bool {
	for _, t := range targets {
		if strings.HasPrefix(filename, t+"-") {
			return true
		}
	}
	return false
}

// isLatestInstalled reports whether a cache filename corresponds to a currently
// installed latest version of a formula.
func isLatestInstalled(installed []cellar.InstalledPackage, filename string) bool {
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

func pruneEmptyDirs(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() || path == root {
			return nil
		}

		// Recurse first so we can remove parents of children that were just removed
		pruneEmptyDirs(path)

		// Check if it's empty now
		entries, err := os.ReadDir(path)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(path)
		}
		return filepath.SkipDir // Already recursed
	})
}

func dirSize(path string) (int64, error) {
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

func entrySize(path string, e os.DirEntry) (int64, error) {
	info, err := e.Info()
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return dirSize(path)
	}
	return info.Size(), nil
}

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
