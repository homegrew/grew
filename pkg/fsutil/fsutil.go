package fsutil

import (
	"fmt"
	"github.com/homegrew/grew/pkg/safepath"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Default permission modes used when an extracted entry has no explicit mode.
const (
	defaultDirMode  os.FileMode = 0o755
	defaultFileMode os.FileMode = 0o644
)

// Lock acquires an exclusive advisory lock on the given file.
// It blocks until the lock is acquired.
func Lock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// TryLock attempts to acquire an exclusive advisory lock on the given file
// without blocking. It returns syscall.EWOULDBLOCK if the lock is already held.
func TryLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// Unlock releases an advisory lock on the given file.
func Unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// CopyTree recursively copies a directory tree from src to dst.
// Symlinks are preserved but validated to not escape the destination.
func CopyTree(src, dst string) error {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve dest: %w", err)
	}
	absDst = filepath.Clean(absDst)
	if err := safepath.SafeAbsolutePath(absDst); err != nil {
		return fmt.Errorf("invalid destination root %q: %w", absDst, err)
	}

	absSrc, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve src: %w", err)
	}
	absSrc = filepath.Clean(absSrc)
	if err := safepath.SafeAbsolutePath(absSrc); err != nil {
		return fmt.Errorf("invalid source root %q: %w", absSrc, err)
	}

	return filepath.WalkDir(absSrc, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(absSrc, path)
		if err != nil {
			return err
		}
		rel = filepath.Clean(rel)
		target, err := safepath.SafeJoin(absDst, rel)
		if err != nil {
			return fmt.Errorf("refusing to copy outside destination root: %w", err)
		}
		if err := safepath.SafeAbsolutePath(target); err != nil {
			return fmt.Errorf("invalid copy target %q: %w", target, err)
		}

		// Ensure that the computed target path stays within the destination root.
		if !safepath.IsSubpath(absDst, target) {
			return fmt.Errorf("refusing to copy outside destination root: %s", target)
		}

		if d.Type()&os.ModeSymlink != 0 {
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
			if safepath.IsSubpath(absSrc, resolvedSource) {
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
				slog.Warn(fmt.Sprintf("fsutil: skipping symlink %q (target %q, resolved to %q, escapes source tree %q)", path, link, resolvedSource, absSrc))
				return nil
			}
			resolvedDest = filepath.Clean(resolvedDest)

			if !safepath.IsSubpath(absDst, resolvedDest) {
				// Skip symlinks that escape — don't fail, just log and skip.
				slog.Warn(fmt.Sprintf("fsutil: skipping symlink %q (target would resolve to %q, outside destination tree %q)", path, resolvedDest, absDst))
				return nil
			}
			if rel == "." {
				slog.Warn(fmt.Sprintf("fsutil: skipping symlink %q at destination root %q", path, absDst))
				return nil
			}
			return os.Symlink(link, target)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			dirMode := SanitizeMode(info.Mode(), true)
			if err := os.MkdirAll(target, dirMode); err != nil {
				// If the path already exists, ensure it's a directory and update permissions.
				if os.IsExist(err) {
					if subErr := safepath.CheckSubpath(absDst, target); subErr != nil {
						return fmt.Errorf("refusing to stat outside destination root: %w", subErr)
					}
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
				} else {
					return err
				}
			}
			_ = os.Chtimes(target, info.ModTime(), info.ModTime())
			return nil
		}

		return CopyFileWithinRoot(path, target, absDst, SanitizeMode(info.Mode(), false))
	})
}

// CopyFileWithinRoot copies a single file from src to dst, ensuring
// that the destination lies within a specified root directory. If root is
// empty, a root is derived from the destination's parent directory.
func CopyFileWithinRoot(src, dst, root string, mode os.FileMode) error {
	// Normalize source to an absolute, cleaned path to prevent path traversal.
	if src == "" {
		return fmt.Errorf("source path cannot be empty")
	}
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}
	absSrc = filepath.Clean(absSrc)

	srcInfo, err := os.Stat(absSrc)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

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
		if !safepath.IsSubpath(absRoot, absDst) {
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
			if rootErr == nil && !safepath.IsSubpath(resolvedRoot, resolvedDstDir) {
				return fmt.Errorf("refusing to copy file outside destination root via symlink: %s", absDst)
			}
		}
	}

	in, err := os.Open(absSrc)
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
	if cerr == nil {
		_ = os.Chtimes(absDst, srcInfo.ModTime(), srcInfo.ModTime())
	}
	return cerr
}

// CopyFile copies a single file from src to dst.
// It is a convenience wrapper around CopyFileWithinRoot that skips the
// symlink-containment check (no cross-tree root enforcement).
func CopyFile(src, dst string, mode os.FileMode) error {
	return CopyFileWithinRoot(src, dst, "", mode)
}

// WriteFileAtomic writes data to dst atomically: it writes to a temporary file
// in the same directory, applies mode, then renames over dst. The temp file is
// cleaned up if any step fails, so dst is never left partially written.
func WriteFileAtomic(dst string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".grew-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
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

// FormatSize returns a human-readable string representation of the given size in bytes.
func FormatSize(b int64) string {
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

// DiskUsage returns the total size and number of files in the given directory.
func DiskUsage(root string) (size int64, files int64, err error) {
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			size += info.Size()
			files++
		}
		return nil
	})
	return
}

// PruneEmptyDirs recursively removes empty directories starting from the given root.
func PruneEmptyDirs(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() || path == root {
			return nil
		}

		// Recurse first so we can remove parents of children that were just removed
		PruneEmptyDirs(path)

		// Check if it's empty now
		entries, err := os.ReadDir(path)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(path)
		}
		return filepath.SkipDir // Already recursed
	})
}

// DirSize returns the total size of all files in the given directory and its subdirectories.
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

// EntrySize returns the size of a directory entry (file or directory).
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

// RemoveIfWithinAllowed removes a file if it is located within the temporary or cache directories.
func RemoveIfWithinAllowed(tmpDir, cacheDir, candidate string) error {
	if tmpDir == "" || cacheDir == "" || candidate == "" {
		return nil
	}
	cleanTmp, err := filepath.Abs(filepath.Clean(tmpDir))
	if err != nil {
		return err
	}
	cleanCache, err := filepath.Abs(filepath.Clean(cacheDir))
	if err != nil {
		return err
	}
	cleanCandidate, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return err
	}

	if strings.HasPrefix(cleanCandidate, cleanTmp) || strings.HasPrefix(cleanCandidate, cleanCache) {
		return os.Remove(cleanCandidate)
	}
	return fmt.Errorf("refusing to remove file outside of allowed directories: %s", cleanCandidate)
}
