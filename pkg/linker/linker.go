package linker

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/validation"
)

// Linker creates and removes the prefix symlinks that expose an installed keg.
// It is stateless apart from Paths, which locates the cellar and the shared
// prefix directories (opt/, bin/, lib/, include/, share/) it operates on.
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

// Link is a convenience wrapper over [Linker.LinkWithOpts] that links the keg
// at Cellar/<name>/<version>, passing only the keg-only flag and otherwise
// using default options.
func (l *Linker) Link(name, version string, kegOnly bool) error {
	return l.LinkWithOpts(name, version, LinkOpts{KegOnly: kegOnly})
}

// LinkWithOpts links the keg at Cellar/<name>/<version> into the prefix. It
// always (re)creates the opt/<name> symlink, then — unless the formula is
// keg-only — populates bin/, lib/, include/, and share/ with symlinks into the
// keg. Existing links owned by the same formula are replaced; links owned by a
// different formula cause an error unless opts.Overwrite is set. A keg-only
// formula receives only its opt link. As a backstop, the link is refused when
// another member of the same version family already owns the shared links
// (see [Linker.checkFamilyConflict]). The name, version, and resolved keg path
// are all validated to stay within the cellar before any link is created.
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
	absOptLink, err := filepath.Abs(optLink)
	if err != nil {
		return fmt.Errorf("resolve opt link path: %w", err)
	}
	absOptLink = filepath.Clean(absOptLink)
	if !safepath.IsSubpath(l.Paths.Opt, absOptLink) || !safepath.IsSubpath(l.Paths.Root, absOptLink) {
		return fmt.Errorf("refusing to operate outside managed prefix: %s", absOptLink)
	}
	if opts.DryRun {
		fmt.Printf("Would link: %s -> %s\n", absOptLink, kegPath)
	} else {
		if err := os.Remove(absOptLink); err != nil && !os.IsNotExist(err) {
			slog.Error("failed to remove existing opt link", "link", absOptLink, "error", err)
			return fmt.Errorf("remove existing opt link: %w", err)
		}
		if err := os.Symlink(kegPath, absOptLink); err != nil {
			slog.Error("failed to create opt link", "link", absOptLink, "target", kegPath, "error", err)
			return fmt.Errorf("create opt link: %w", err)
		}
	}

	if opts.KegOnly && !opts.Force {
		return nil
	}

	// Version-family conflict guard (defense-in-depth): refuse to link a
	// formula whose base name is already linked into bin/lib/include/share by a
	// DIFFERENT keg in the same version family (e.g. linking node@24 while node
	// is linked). Part 1 makes versioned formulas keg-only, but a hand-written
	// definition with keg_only:false would bypass that; this guard protects the
	// shared link tree regardless. Overwrite/Force bypasses it and lets the
	// per-file overwrite logic replace the symlinks.
	if !opts.Overwrite && !opts.Force {
		if err := l.checkFamilyConflict(name, opts); err != nil {
			return err
		}
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

// Unlink is a convenience wrapper over [Linker.UnlinkWithOpts] that removes a
// formula's links using default options.
func (l *Linker) Unlink(name string) error {
	return l.UnlinkWithOpts(name, UnlinkOpts{})
}

// UnlinkWithOpts removes the opt/<name> symlink and every symlink in bin/, lib/,
// include/, and share/ that resolves into Cellar/<name>/, leaving links owned by
// other formulas untouched and pruning directories that become empty. Missing
// directories are not an error, so unlinking is safe to call on a partially
// linked or already-unlinked formula.
func (l *Linker) UnlinkWithOpts(name string, opts UnlinkOpts) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid formula name: %q", name)
	}

	optLink := filepath.Clean(filepath.Join(l.Paths.Opt, name))
	if !safepath.IsSubpath(l.Paths.Opt, optLink) {
		return fmt.Errorf("refusing to unlink path outside opt directory: %q", optLink)
	}
	if opts.DryRun {
		if target, err := os.Readlink(optLink); err == nil {
			fmt.Printf("Would unlink: %s -> %s\n", optLink, target)
		}
	} else {
		if err := os.Remove(optLink); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove opt symlink %q: %w", optLink, err)
		}
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

// unlinkDirWithOpts recursively removes, within dir, every symlink whose
// resolved target begins with cellarPrefix (i.e. belongs to the formula being
// unlinked), and deletes subdirectories left empty afterward.
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
				if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove %s: %w", fullPath, err)
				}
			}
		}
	}
	return nil
}

// IsLinked reports whether the formula currently owns its opt/<name> symlink,
// which grew treats as the canonical signal that a formula is linked. It returns
// false for an invalid name or a missing link.
func (l *Linker) IsLinked(name string) bool {
	if !validation.IsValidName(name) {
		return false
	}
	optLink := filepath.Join(l.Paths.Opt, name)
	_, err := os.Readlink(optLink)
	return err == nil
}

// checkFamilyConflict scans opt/* for an already-linked formula in the same
// version family as name (sharing a base name, e.g. "node" vs "node@24") and
// returns an error if one is actively occupying the shared link tree.
//
// Design choice for the keg-only-vs-linked distinction: the presence of
// opt/<other> alone is NOT sufficient, because a keg-only formula also has an
// opt symlink yet owns no bin/lib/include/share links and therefore does not
// compete. Rather than re-loading each formula definition to read its keg_only
// flag (costly and would require the loader here), we use the conservative,
// on-disk signal that actually matters: <other> is only a real conflict if at
// least one symlink in bin/ resolves into Cellar/<other>/. That is exactly the
// state we are protecting against, and a keg-only family member (opt link but
// no bin links) produces no false conflict.
func (l *Linker) checkFamilyConflict(name string, opts LinkOpts) error {
	base := formula.BaseName(name)

	// Guard against path-injection: canonicalize and verify Opt is within Root
	// before calling os.ReadDir.
	rootAbs, err := filepath.Abs(l.Paths.Root)
	if err != nil {
		return fmt.Errorf("invalid root path: %w", err)
	}
	rootAbs = filepath.Clean(rootAbs)
	if err := safepath.SafeAbsolutePath(rootAbs); err != nil {
		return fmt.Errorf("invalid root path")
	}

	optAbs, err := filepath.Abs(l.Paths.Opt)
	if err != nil {
		return fmt.Errorf("invalid opt path: %w", err)
	}
	optAbs = filepath.Clean(optAbs)
	if err := safepath.SafeAbsolutePath(optAbs); err != nil {
		return fmt.Errorf("invalid opt path")
	}

	rel, err := filepath.Rel(rootAbs, optAbs)
	if err != nil {
		return fmt.Errorf("invalid opt path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid opt path")
	}

	entries, err := os.ReadDir(optAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read opt dir: %w", err)
	}

	for _, e := range entries {
		other := e.Name()
		if other == name {
			continue
		}
		if !validation.IsValidName(other) {
			continue
		}
		if formula.BaseName(other) != base {
			continue
		}
		// Same family, different formula. Only a conflict if it actually owns
		// bin links (i.e. is not effectively keg-only).
		if !l.hasBinLinks(other) {
			continue
		}
		if opts.DryRun {
			fmt.Printf("Would conflict: %s is already linked from the same version family (%s)\n", other, base)
			continue
		}
		return fmt.Errorf("cannot link %s: %s is already linked from the same version family; unlink it first or use --overwrite", name, other)
	}
	return nil
}

// hasBinLinks reports whether any symlink directly under bin/ resolves into
// Cellar/<other>/, i.e. the formula currently owns bin links. Validated name
// and SafeJoin keep all path construction within the trusted root.
func (l *Linker) hasBinLinks(other string) bool {
	if !validation.IsValidName(other) {
		return false
	}
	cellarPrefix := filepath.Join(l.Paths.Cellar, other) + string(filepath.Separator)

	// Canonicalize and validate Root/Bin before filesystem access.
	rootAbs, err := filepath.Abs(l.Paths.Root)
	if err != nil {
		return false
	}
	rootAbs = filepath.Clean(rootAbs)

	binAbs, err := filepath.Abs(l.Paths.Bin)
	if err != nil {
		return false
	}
	binAbs = filepath.Clean(binAbs)

	// Guard against path-injection: Bin must be Root or a descendant of Root.
	if !safepath.IsSubpath(rootAbs, binAbs) {
		return false
	}

	entries, err := os.ReadDir(binAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		// Fail closed: an unexpected read error must not let the conflict guard
		// silently pass and permit a double-link. Conservatively report that the
		// family member owns bin links so the caller refuses to link.
		slog.Warn("linker: unable to inspect bin directory for existing links; refusing to link conservatively", "bin", binAbs, "error", err)
		return true
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(e.Name()); err != nil {
			continue
		}
		fullPath, err := safepath.SafeJoin(binAbs, e.Name())
		if err != nil {
			continue
		}
		// Guard against path-injection: canonicalize and verify fullPath remains under binAbs
		// before calling os.Readlink. Prefer symlink-evaluated paths when available.
		canonBin := binAbs
		if resolvedBin, err := filepath.EvalSymlinks(binAbs); err == nil {
			canonBin = filepath.Clean(resolvedBin)
		}
		canonParent := filepath.Dir(fullPath)
		if resolvedParent, err := filepath.EvalSymlinks(canonParent); err == nil {
			canonParent = filepath.Clean(resolvedParent)
		}
		canonFull := filepath.Join(canonParent, filepath.Base(fullPath))
		if !safepath.IsSubpath(canonBin, canonFull) {
			continue
		}
		target, err := os.Readlink(fullPath)
		if err != nil {
			continue // not a symlink
		}
		if strings.HasPrefix(resolveLink(binAbs, target), cellarPrefix) {
			return true
		}
	}
	return false
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

// configuredAllowedRoots returns allowed roots from environment or secure defaults.
func configuredAllowedRoots() []string {
	if raw := strings.TrimSpace(os.Getenv("HOMEGREW_ALLOWED_ROOTS")); raw != "" {
		parts := strings.Split(raw, string(os.PathListSeparator))
		roots := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				roots = append(roots, p)
			}
		}
		if len(roots) > 0 {
			return roots
		}
	}
	return []string{"/usr/local/homegrew", "/opt/homegrew"}
}

func normalizeAndValidateAllowedRoots(roots []string) []string {
	allowed := make([]string, 0, len(roots))
	for _, r := range roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if err := safepath.SafeAbsolutePath(abs); err != nil {
			continue
		}
		allowed = append(allowed, abs)
	}
	return allowed
}

func isAllowedRoot(root string) bool {
	allowed := normalizeAndValidateAllowedRoots(configuredAllowedRoots())
	for _, r := range allowed {
		if root == r {
			return true
		}
	}
	return false
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
	if !isAllowedRoot(absRoot) {
		slog.Error("rejecting non-system root for directory check", "root", absRoot)
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

// isWithinRoot reports whether candidate is root itself or lies beneath it,
// rejecting any path that would require traversing upward out of root. It is the
// inline containment barrier used throughout this package before a constructed
// path reaches a filesystem call.
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

// linkDirWithOpts recursively links the contents of srcDir (a keg's bin/, lib/,
// include/, or share/) into destDir. Plain files are symlinked individually;
// directories shared between formulas (e.g. lib/pkgconfig) are materialized as
// real directories and recursed into so multiple formulas can coexist. Existing
// destinations owned by the same formula are replaced silently, while those
// owned by another formula error unless opts.Overwrite is set. destRoot anchors
// every constructed path inside the trusted prefix; cellarPath and formulaName
// identify the owning keg for conflict resolution.
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
			// Re-assert containment at sink site so destPath cannot be used for
			// filesystem operations outside destRoot.
			if err := safepath.CheckSubpath(destRoot, destPath); err != nil {
				return fmt.Errorf("refusing directory operation outside root %s: %s: %w", destRoot, destPath, err)
			}
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
	// Explicit sink-adjacent containment guard for the exact path used by ReadDir.
	if !isWithinRoot(absRoot, safeTarget) {
		return fmt.Errorf("refusing to read target dir outside root %s: %s", absRoot, safeTarget)
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
