package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Default permission modes used when an extracted entry has no explicit mode.
const (
	defaultDirMode  os.FileMode = 0o755
	defaultFileMode os.FileMode = 0o644
)

// isWithinRoot reports whether candidate is within the directory tree rooted at root.
// It mirrors the symlink escape validation logic used elsewhere to ensure consistency.
func isWithinRoot(root, candidate string) bool {
	normalizedRoot := filepath.Clean(root)
	normalizedCandidate := filepath.Clean(candidate)

	if runtime.GOOS == "windows" {
		normalizedRoot = strings.ToLower(normalizedRoot)
		normalizedCandidate = strings.ToLower(normalizedCandidate)
	}

	sep := string(filepath.Separator)
	return normalizedCandidate == normalizedRoot || strings.HasPrefix(normalizedCandidate, normalizedRoot+sep)
}

// CopyTree recursively copies a directory tree from src to dst.
// Symlinks are preserved but validated to not escape the destination.
func CopyTree(src, dst string) error {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve dest: %w", err)
	}
	absDst = filepath.Clean(absDst)

	absSrc, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve src: %w", err)
	}
	absSrc = filepath.Clean(absSrc)

	return filepath.Walk(absSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(absSrc, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		target := filepath.Join(absDst, rel)
		target = filepath.Clean(target)

		// Ensure that the computed target path stays within the destination root.
		if !isWithinRoot(absDst, target) {
			return fmt.Errorf("refusing to copy outside destination root: %s", target)
		}

		// Detect symlinks via Lstat since Walk follows them
		symlinkInfo, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}

		if symlinkInfo.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// Validate the symlink won't escape the destination tree.
			resolvedSource := link
			if !filepath.IsAbs(resolvedSource) {
				// Relative symlinks are resolved relative to the symlink's own directory.
				resolvedSource = filepath.Join(filepath.Dir(path), link)
			}
			resolvedSource = filepath.Clean(resolvedSource)

			// Map the resolved source path into the destination tree (if possible)
			var resolvedDest string
			if isWithinRoot(absSrc, resolvedSource) {
				relFromSrc, relErr := filepath.Rel(absSrc, resolvedSource)
				if relErr != nil {
					return relErr
				}
				relFromSrc = filepath.Clean(relFromSrc)
				resolvedDest = filepath.Join(absDst, relFromSrc)
			} else if filepath.IsAbs(resolvedSource) {
				// Absolute symlinks are validated as-is against the destination root.
				resolvedDest = resolvedSource
			} else {
				// Cannot sensibly map a non-absolute path outside the source root; skip it.
				fmt.Fprintf(os.Stderr, "fsutil: skipping symlink %q (target %q, resolved to %q, escapes source tree %q)\n", path, link, resolvedSource, absSrc)
				return nil
			}
			resolvedDest = filepath.Clean(resolvedDest)

			if !isWithinRoot(absDst, resolvedDest) {
				// Skip symlinks that escape — don't fail, just log and skip.
				fmt.Fprintf(os.Stderr, "fsutil: skipping symlink %q (target would resolve to %q, outside destination tree %q)\n", path, resolvedDest, absDst)
				return nil
			}
			return os.Symlink(link, target)
		}

		if info.IsDir() {
			dirMode := SanitizeMode(info.Mode(), true)
			if err := os.MkdirAll(target, dirMode); err != nil {
				// If the path already exists, ensure it's a directory and update permissions.
				if os.IsExist(err) {
					st, statErr := os.Stat(target)
					if statErr != nil {
						return statErr
					}
					if !st.IsDir() {
						return err
					}
					if chmodErr := os.Chmod(target, dirMode); chmodErr != nil {
						return chmodErr
					}
					return nil
				}
				return err
			}
			return nil
		}

		return CopyFileWithinRoot(path, target, absDst, SanitizeMode(info.Mode(), false))
	})
}

// CopyFileWithinRoot copies a single file from src to dst, ensuring
// that the destination lies within a specified root directory. If root is
// empty, a root is derived from the destination's parent directory.
func CopyFileWithinRoot(src, dst, root string, mode os.FileMode) error {
	// Normalize destination to an absolute, cleaned path before use.
	if dst == "" {
		return fmt.Errorf("destination path cannot be empty")
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}
	absDst = filepath.Clean(absDst)

	// If no root is provided, derive one from the destination's parent directory.
	if root == "" {
		root = filepath.Dir(absDst)
	}

	// Ensure the destination stays within the resolved root.
	if root != "" {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("resolve destination root: %w", err)
		}
		absRoot = filepath.Clean(absRoot)

		// First, perform a cheap lexical containment check.
		if !isWithinRoot(absRoot, absDst) {
			return fmt.Errorf("refusing to copy file outside destination root: %s", absDst)
		}

		// Then, resolve symlinks in both the destination directory and the root to
		// ensure the real path is still contained. This prevents a case like
		// <root>/sub/file where "sub" is a symlink pointing outside <root>.
		// Both sides are resolved for consistent comparison; on platforms like macOS
		// the temp directory path itself is a symlink (e.g. /var → /private/var) and
		// comparing a resolved dstDir against an unresolved root would produce false positives.
		dstDir := filepath.Dir(absDst)
		resolvedDstDir, dstDirErr := filepath.EvalSymlinks(dstDir)
		if dstDirErr != nil && !os.IsNotExist(dstDirErr) {
			return fmt.Errorf("resolve destination directory: %w", dstDirErr)
		}
		// When dstDir does not yet exist (new file creation), skip the symlink-resolved
		// check and rely on the lexical containment check above.
		if dstDirErr == nil {
			resolvedRoot, rootErr := filepath.EvalSymlinks(absRoot)
			if rootErr != nil && !os.IsNotExist(rootErr) {
				return fmt.Errorf("resolve destination root symlinks: %w", rootErr)
			}
			if rootErr == nil && !isWithinRoot(resolvedRoot, resolvedDstDir) {
				return fmt.Errorf("refusing to copy file outside destination root via symlink: %s", absDst)
			}
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Validate destination before truncating/creating to avoid overwriting non-regular files.
	if info, statErr := os.Lstat(absDst); statErr == nil {
		// Destination exists; ensure it's a regular file before overwriting.
		if !info.Mode().IsRegular() {
			return fmt.Errorf("destination %q is not a regular file", absDst)
		}
	} else if !os.IsNotExist(statErr) {
		// An unexpected error occurred while checking the destination.
		return statErr
	}

	out, err := os.OpenFile(absDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	var cerr error
	defer func() {
		if closeErr := out.Close(); closeErr != nil && cerr == nil {
			cerr = closeErr
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		cerr = err
	}
	return cerr
}

// CopyFile copies a single file from src to dst.
// It is a convenience wrapper around CopyFileWithinRoot that skips the
// symlink-containment check (no cross-tree root enforcement).
func CopyFile(src, dst string, mode os.FileMode) error {
	return CopyFileWithinRoot(src, dst, "", mode)
}

// SanitizeMode applies a umask to archive-extracted file modes,
// stripping setuid/setgid/sticky bits and world-write.
func SanitizeMode(mode os.FileMode, isDir bool) os.FileMode {
	mode &^= os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	mode &^= 0002 // strip world-write
	if isDir {
		if mode == 0 {
			return defaultDirMode
		}
		return mode | 0700
	}
	if mode == 0 {
		return defaultFileMode
	}
	return mode
}
