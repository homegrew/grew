package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/flags"
)

func runCleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.Register(fs)
	dryRun := fs.Bool("dry-run", false, "Show what would be removed")
	fs.BoolVar(dryRun, "n", false, "Show what would be removed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	targets := fs.Args()

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	var totalBytes int64

	// Clean old version kegs
	installed, err := cel.List()
	if err != nil {
		return err
	}

	// Filter to specific targets if provided
	if len(targets) > 0 {
		targetSet := make(map[string]bool, len(targets))
		for _, t := range targets {
			targetSet[t] = true
		}
		var filtered []cellar.InstalledPackage
		for _, pkg := range installed {
			if targetSet[pkg.Name] {
				filtered = append(filtered, pkg)
			}
		}
		installed = filtered
	}

	for _, pkg := range installed {
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

	// Clean tmp directory, but only if it is safely located under the grew root.
	if paths.IsUnderRoot(paths.Tmp) {
		tmpEntries, err := os.ReadDir(paths.Tmp)
		if err == nil {
			for _, e := range tmpEntries {
				path := filepath.Join(paths.Tmp, e.Name())
				size, _ := entrySize(path, e)
				totalBytes += size
				if *dryRun {
					fmt.Printf("Would remove: %s (%s)\n", path, formatSize(size))
				} else {
					slog.Debug("removing temp file " + e.Name())
					if err := os.RemoveAll(path); err != nil {
						slog.Warn(fmt.Sprintf("could not remove %s: %v", path, err))
					} else {
						fmt.Printf("Removing: %s (%s)\n", path, formatSize(size))
					}
				}
			}
		}
	} else {
		slog.Debug("skipping cleanup of tmp directory outside grew root: " + paths.Tmp)
	}

	if totalBytes == 0 {
		fmt.Println("Already clean, nothing to do.")
	} else if *dryRun {
		fmt.Printf("==> Would free %s\n", formatSize(totalBytes))
	} else {
		fmt.Printf("==> Freed %s\n", formatSize(totalBytes))
	}

	return nil
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
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
