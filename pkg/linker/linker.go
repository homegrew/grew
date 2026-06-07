package linker

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/validation"
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
			slog.Error("failed to create opt link", "link", optLink, "target", kegPath, "error", err)
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
		{filepath.Join(kegPath, "share"), l.Paths.Share},
	}

	for _, sd := range subdirs {
		if err := linkDirWithOpts(sd.src, sd.dest, l.Paths.Root, l.Paths.Cellar, name, opts); err != nil {
			slog.Error("failed to link subdir", "src", sd.src, "dest", sd.dest, "error", err)
			return fmt.Errorf("link %s: %w", sd.src, err)
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
	dirs := []string{l.Paths.Bin, l.Paths.Lib, l.Paths.Include, l.Paths.Share}

	for _, dir := range dirs {
		if err := unlinkDirWithOpts(dir, cellarPrefix, opts); err != nil {
			return err
		}
	}
	return nil
}

func unlinkDirWithOpts(dir, cellarPrefix string, opts UnlinkOpts) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		fullPath := filepath.Join(dir, e.Name())

		if e.IsDir() {
			// Recurse into subdirectories (e.g. lib/pkgconfig or share/man)
			if err := unlinkDirWithOpts(fullPath, cellarPrefix, opts); err != nil {
				return err
			}

			// If empty, clean it up
			if !opts.DryRun {
				_ = os.Remove(fullPath) // Ignore errors, it just means not empty
			}
			continue
		}

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

// destIsDir returns true if destPath is a real directory or a symlink that
// points to a directory inside the configured root.
func destIsDir(root, destPath string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		slog.Error("failed to resolve root path", "root", root, "error", err)
		return false
	}
	absRoot = filepath.Clean(absRoot)
	if err := safepath.SafeAbsolutePath(absRoot); err != nil {
		slog.Error("failed to validate root path", "root", absRoot, "error", err)
		return false
	}

	absDest, err := filepath.Abs(destPath)
	if err != nil {
		slog.Error("failed to resolve dest path", "dest", destPath, "error", err)
		return false
	}
	absDest = filepath.Clean(absDest)
	if !isWithinRoot(absRoot, absDest) {
		return false
	}

	resolvedDest := absDest
	if eval, err := filepath.EvalSymlinks(absDest); err == nil {
		resolvedDest = filepath.Clean(eval)
		if !isWithinRoot(absRoot, resolvedDest) {
			return false
		}
	}
	if err := safepath.SafeAbsolutePath(resolvedDest); err != nil {
		return false
	}

	// Rebuild the path handed to os.Stat from the trusted root plus the
	// validated relative remainder via safepath.SafeJoin (join + containment
	// in one call, the repo's sanctioned barrier). This severs the taint that
	// would otherwise flow from the original destPath argument into the stat
	// sink, in addition to the containment checks above.
	rel, err := filepath.Rel(absRoot, resolvedDest)
	if err != nil {
		return false
	}
	statPath := absRoot
	if rel != "." {
		statPath, err = safepath.SafeJoin(absRoot, rel)
		if err != nil {
			return false
		}
	}

	fi, err := os.Stat(statPath) // built from absRoot via SafeJoin, contained above
	return err == nil && fi.IsDir()
}

func isWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func linkDirWithOpts(srcDir, destDir, destRoot, cellarPath, formulaName string, opts LinkOpts) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", srcDir, err)
	}

	// Anchor destDir to the trusted install root before any path is built from
	// it. destDir arrives either as a configured prefix subdir (bin/lib/...) or,
	// on recursion, as a SafeJoin result; re-validating it as a clean absolute
	// path within destRoot gives every downstream SafeJoin(destDir, ...) a
	// root-anchored base and severs taint flowing through the parameter. Done
	// after the ReadDir short-circuit so a missing source dir never trips it.
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve dest dir %s: %w", destDir, err)
	}
	absDestDir = filepath.Clean(absDestDir)
	if err := safepath.SafeAbsolutePath(destRoot); err != nil {
		return fmt.Errorf("invalid dest root %s: %w", destRoot, err)
	}
	if err := safepath.CheckSubpath(destRoot, absDestDir); err != nil {
		return fmt.Errorf("dest dir %s escapes root %s: %w", absDestDir, destRoot, err)
	}
	destDir = absDestDir

	for _, e := range entries {
		// Entry names are read from keg contents on disk; validate each as a
		// single, traversal-free path component and build the joined paths
		// through safepath.SafeJoin so they are confirmed within their parent
		// directories before any filesystem use (defense in depth against
		// path injection).
		if err := safepath.SafePathComponent(e.Name()); err != nil {
			return fmt.Errorf("unsafe entry %q in %s: %w", e.Name(), srcDir, err)
		}
		srcPath, err := safepath.SafeJoin(srcDir, e.Name())
		if err != nil {
			return fmt.Errorf("entry %q escapes %s: %w", e.Name(), srcDir, err)
		}
		destPath, err := safepath.SafeJoin(destDir, e.Name())
		if err != nil {
			return fmt.Errorf("entry %q escapes %s: %w", e.Name(), destDir, err)
		}
		// Inline containment guard on the exact value reaching every destPath
		// sink below (Lstat, Remove, MkdirAll, Symlink, unsymDir, recursion).
		// This is the CodeQL-recognized barrier for go/path-injection; the
		// SafeJoin above is kept as defense in depth.
		if !isWithinRoot(destRoot, destPath) {
			return fmt.Errorf("refusing to operate outside root %s: %s", destRoot, destPath)
		}

		// grew does not install info files or manpages
		if strings.HasSuffix(srcDir, "/share") || strings.HasSuffix(srcDir, "/share/") {
			if e.Name() == "info" || e.Name() == "man" {
				continue
			}
		}

		// Source is a directory (e.g. lib/pkgconfig). These are shared
		// directories where multiple formulas contribute files. Instead
		// of symlinking the directory, ensure a real directory exists at
		// the destination and recurse to link individual files.
		if e.IsDir() {
			if info, err := os.Lstat(destPath); err == nil {
				if info.Mode()&os.ModeSymlink != 0 && destIsDir(filepath.Dir(cellarPath), destPath) {
					// Destination is a symlink to a directory (from another
					// formula). Replace it with a real directory and migrate
					// the contents so both formulas' files coexist.
					if !opts.DryRun {
						if err := unsymDir(destPath, filepath.Dir(cellarPath)); err != nil {
							return fmt.Errorf("expand shared dir %s: %w", destPath, err)
						}
					}
				} else if info.Mode()&os.ModeSymlink != 0 {
					// Symlink to a non-directory — treat as conflict.
					if !opts.Overwrite {
						return fmt.Errorf("cannot link %s: %s already linked by another formula (use --overwrite to force)", e.Name(), destPath)
					}
					if !opts.DryRun {
						os.Remove(destPath)
					}
				}
				// else: already a real directory, good.
			}
			if !opts.DryRun {
				if err := os.MkdirAll(destPath, 0755); err != nil {
					return fmt.Errorf("create shared dir %s: %w", destPath, err)
				}
			}
			if err := linkDirWithOpts(srcPath, destPath, destRoot, cellarPath, formulaName, opts); err != nil {
				return err
			}
			continue
		}

		// Source is a file or symlink — link it directly.
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

// unsymDir replaces a symlink-to-directory with a real directory containing
// symlinks to each entry in the original target. This allows multiple
// formulas to share the directory (e.g. lib/pkgconfig).
func unsymDir(symlinkPath, root string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve root %s: %w", root, err)
	}
	absRoot = filepath.Clean(absRoot)

	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		realRoot = absRoot
	} else {
		realRoot = filepath.Clean(realRoot)
	}

	absSymlinkPath, err := filepath.Abs(symlinkPath)
	if err != nil {
		return fmt.Errorf("resolve path %s: %w", symlinkPath, err)
	}
	absSymlinkPath = filepath.Clean(absSymlinkPath)
	if err := safepath.SafeAbsolutePath(realRoot); err != nil {
		return fmt.Errorf("invalid root path %s: %w", realRoot, err)
	}
	if err := safepath.SafeAbsolutePath(absSymlinkPath); err != nil {
		return fmt.Errorf("invalid symlink path %s: %w", absSymlinkPath, err)
	}
	if !isWithinRoot(realRoot, absSymlinkPath) {
		return fmt.Errorf("refusing to modify path outside root: %s", absSymlinkPath)
	}

	parent := filepath.Dir(absSymlinkPath)
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve parent %s: %w", parent, err)
	}
	realParent = filepath.Clean(realParent)
	realSymlinkPath := filepath.Clean(filepath.Join(realParent, filepath.Base(absSymlinkPath)))

	if !isWithinRoot(realRoot, realSymlinkPath) {
		return fmt.Errorf("refusing to expand path outside root: %s", realSymlinkPath)
	}

	target, err := os.Readlink(absSymlinkPath)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(absSymlinkPath), target)
	}
	if absTarget, err := filepath.Abs(target); err == nil {
		target = filepath.Clean(absTarget)
	} else {
		target = filepath.Clean(target)
	}

	if !isWithinRoot(absRoot, target) {
		return fmt.Errorf("refusing to use symlink target outside root: %s", target)
	}
	if err := safepath.SafeAbsolutePath(target); err != nil {
		return fmt.Errorf("invalid symlink target %s: %w", target, err)
	}

	// Rebuild the path handed to os.Stat/os.ReadDir from the trusted root plus
	// the validated relative remainder via safepath.SafeJoin (join + containment
	// in one call, the repo's sanctioned barrier). target is read off disk via
	// os.Readlink, so this severs that taint at the sink in addition to the
	// isWithinRoot guard above.
	rel, err := filepath.Rel(absRoot, target)
	if err != nil {
		return fmt.Errorf("relativize target %s: %w", target, err)
	}
	safeTarget := absRoot
	if rel != "." {
		safeTarget, err = safepath.SafeJoin(absRoot, rel)
		if err != nil {
			return fmt.Errorf("target escapes root %s: %w", target, err)
		}
	}

	info, err := os.Stat(safeTarget) // built from absRoot via SafeJoin, contained above
	if err != nil {
		return fmt.Errorf("stat target dir %s: %w", safeTarget, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("target is not a directory: %s", safeTarget)
	}

	entries, err := os.ReadDir(safeTarget)
	if err != nil {
		return fmt.Errorf("read target dir %s: %w", safeTarget, err)
	}

	// Inline containment guard on the exact value reaching the Remove/MkdirAll
	// sinks, the CodeQL-recognized barrier for go/path-injection (absSymlinkPath
	// is derived from the symlinkPath argument).
	if !isWithinRoot(realRoot, absSymlinkPath) {
		return fmt.Errorf("refusing to replace path outside root %s: %s", realRoot, absSymlinkPath)
	}
	if err := os.Remove(absSymlinkPath); err != nil {
		return err
	}
	if err := os.MkdirAll(absSymlinkPath, 0755); err != nil {
		return err
	}

	for _, e := range entries {
		if err := safepath.SafePathComponent(e.Name()); err != nil {
			return fmt.Errorf("unsafe entry name %q in shared dir: %w", e.Name(), err)
		}
		src, err := safepath.SafeJoin(safeTarget, e.Name())
		if err != nil {
			return fmt.Errorf("entry %q escapes %s: %w", e.Name(), safeTarget, err)
		}
		dst, err := safepath.SafeJoin(absSymlinkPath, e.Name())
		if err != nil {
			return fmt.Errorf("entry %q escapes %s: %w", e.Name(), absSymlinkPath, err)
		}
		// Inline containment guards on the exact src/dst handed to os.Symlink
		// (both are fresh joins not covered by an earlier guard): src within the
		// target's root, dst within the symlink's root. CodeQL-recognized
		// barrier for go/path-injection.
		if !isWithinRoot(absRoot, src) || !isWithinRoot(realRoot, dst) {
			return fmt.Errorf("refusing to link outside root: %s -> %s", dst, src)
		}
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
	}
	return nil
}
