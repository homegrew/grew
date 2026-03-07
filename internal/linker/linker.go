package linker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/validation"
)

type Linker struct {
	Paths config.Paths
}

// LinkOpts controls link behavior.
type LinkOpts struct {
	KegOnly   bool
	Overwrite bool
	DryRun    bool
	Force     bool
}

func (l *Linker) Link(name, version string, kegOnly bool) error {
	return l.LinkWithOpts(name, version, LinkOpts{KegOnly: kegOnly})
}

func (l *Linker) LinkWithOpts(name, version string, opts LinkOpts) error {
	if !validation.IsValidName(name) || !validation.IsValidVersion(version) {
		return fmt.Errorf("invalid formula name or version")
	}

	kegPath := filepath.Join(l.Paths.Cellar, name, version)

	// Verify keg exists and resolves within the cellar.
	realKeg, err := filepath.EvalSymlinks(kegPath)
	if err != nil {
		return fmt.Errorf("keg not found: %s", kegPath)
	}
	realCellar, err := filepath.EvalSymlinks(l.Paths.Cellar)
	if err != nil {
		return fmt.Errorf("cellar path invalid: %w", err)
	}
	if !strings.HasPrefix(realKeg, realCellar+string(filepath.Separator)) {
		return fmt.Errorf("keg %s resolves outside cellar: %s", kegPath, realKeg)
	}

	// Always create opt symlink
	optLink := filepath.Join(l.Paths.Opt, name)
	if opts.DryRun {
		fmt.Printf("Would link: %s -> %s\n", optLink, kegPath)
	} else {
		os.Remove(optLink)
		if err := os.Symlink(kegPath, optLink); err != nil {
			return fmt.Errorf("create opt link: %w", err)
		}
	}

	if opts.KegOnly && !opts.Force {
		return nil
	}

	subdirs := []struct {
		src  string
		dest string
	}{
		{filepath.Join(kegPath, "bin"), l.Paths.Bin},
		{filepath.Join(kegPath, "lib"), l.Paths.Lib},
		{filepath.Join(kegPath, "include"), l.Paths.Include},
	}

	for _, sd := range subdirs {
		if err := linkDirWithOpts(sd.src, sd.dest, l.Paths.Cellar, name, opts); err != nil {
			return err
		}
	}
	return nil
}

// UnlinkOpts controls unlink behavior.
type UnlinkOpts struct {
	DryRun bool
}

func (l *Linker) Unlink(name string) error {
	return l.UnlinkWithOpts(name, UnlinkOpts{})
}

func (l *Linker) UnlinkWithOpts(name string, opts UnlinkOpts) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid formula name: %q", name)
	}

	optLink := filepath.Join(l.Paths.Opt, name)
	if opts.DryRun {
		if target, err := os.Readlink(optLink); err == nil {
			fmt.Printf("Would unlink: %s -> %s\n", optLink, target)
		}
	} else {
		os.Remove(optLink)
	}

	cellarPrefix := filepath.Join(l.Paths.Cellar, name) + string(filepath.Separator)
	dirs := []string{l.Paths.Bin, l.Paths.Lib, l.Paths.Include}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // dir may not exist
		}
		for _, e := range entries {
			fullPath := filepath.Join(dir, e.Name())
			target, err := os.Readlink(fullPath)
			if err != nil {
				continue // not a symlink
			}
			resolved := resolveLink(dir, target)
			if strings.HasPrefix(resolved, cellarPrefix) {
				if opts.DryRun {
					fmt.Printf("Would unlink: %s -> %s\n", fullPath, resolved)
				} else {
					os.Remove(fullPath)
				}
			}
		}
	}
	return nil
}

func (l *Linker) IsLinked(name string) bool {
	if !validation.IsValidName(name) {
		return false
	}
	optLink := filepath.Join(l.Paths.Opt, name)
	_, err := os.Readlink(optLink)
	return err == nil
}

// resolveLink makes a symlink target absolute and cleaned.
func resolveLink(dir, target string) string {
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return filepath.Clean(target)
}

// isOwnedBy checks if a symlink target belongs to the given formula
// by resolving the path and checking it's within Cellar/<name>/.
func isOwnedBy(cellarPath, formulaName, destDir, symlinkTarget string) bool {
	resolved := resolveLink(destDir, symlinkTarget)
	expected := filepath.Join(cellarPath, formulaName) + string(filepath.Separator)
	return strings.HasPrefix(resolved, expected)
}

func linkDirWithOpts(srcDir, destDir, cellarPath, formulaName string, opts LinkOpts) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", srcDir, err)
	}

	for _, e := range entries {
		srcPath := filepath.Join(srcDir, e.Name())
		destPath := filepath.Join(destDir, e.Name())

		if info, err := os.Lstat(destPath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(destPath)
				if isOwnedBy(cellarPath, formulaName, destDir, target) {
					if !opts.DryRun {
						os.Remove(destPath)
					}
				} else if opts.Overwrite {
					if opts.DryRun {
						fmt.Printf("Would overwrite: %s (currently -> %s)\n", destPath, resolveLink(destDir, target))
					} else {
						os.Remove(destPath)
					}
				} else {
					return fmt.Errorf("cannot link %s: %s already linked by another formula (use --overwrite to force)", e.Name(), destPath)
				}
			} else {
				if opts.Overwrite {
					if opts.DryRun {
						fmt.Printf("Would overwrite: %s (regular file)\n", destPath)
					} else {
						os.Remove(destPath)
					}
				} else {
					return fmt.Errorf("cannot link %s: %s already exists and is not a symlink (use --overwrite to force)", e.Name(), destPath)
				}
			}
		}

		if opts.DryRun {
			fmt.Printf("Would link: %s -> %s\n", destPath, srcPath)
		} else {
			if err := os.Symlink(srcPath, destPath); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", destPath, srcPath, err)
			}
		}
	}
	return nil
}
