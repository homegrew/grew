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
				fmt.Fprintf(os.Stderr, "fsutil: skipping symlink %q (resolved to %q) that cannot be mapped into destination %q\n", path, resolvedSource, absDst)
				return nil
			}
			resolvedDest = filepath.Clean(resolvedDest)

			if !isWithinRoot(absDst, resolvedDest) {
				// Skip symlinks that escape — don't fail, just skip silently.
				fmt.Fprintf(os.Stderr, "fsutil: skipping symlink %q (resolved to %q) that escapes destination %q\n", path, resolvedDest, absDst)
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

		// Perform the actual file copy while enforcing that the destination
		// remains within the destination root directory.
		return copyFileWithinRoot(path, target, absDst, SanitizeMode(info.Mode(), false))
	})
}

// copyFileWithinRoot copies a file ensuring that dst stays within the given root.
// This is used by CopyTree to protect against path traversal or symlink escapes
// reaching unexpected locations even if the caller passes an unexpected dst.
func copyFileWithinRoot(src, dst, root string, mode os.FileMode) error {
	if root == "" {
		return fmt.Errorf("copy destination root is empty")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve copy root: %w", err)
	}
	absRoot = filepath.Clean(absRoot)

	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve copy destination: %w", err)
	}
	absDst = filepath.Clean(absDst)

	if !isWithinRoot(absRoot, absDst) {
		return fmt.Errorf("refusing to copy outside destination root: %s", absDst)
	}

	return copyFile(src, absDst, mode)
}

// CopyFile copies a single file from src to dst.
func CopyFile(src, dst string, mode os.FileMode) error {
	// Preserve existing behavior for callers while sharing the implementation.
	return copyFile(src, dst, mode)
}

// copyFile contains the core implementation for copying a single file from src to dst.
// Callers are responsible for any necessary path validation before invoking it.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Validate destination before truncating/creating to avoid overwriting non-regular files.
	if info, statErr := os.Lstat(dst); statErr == nil {
		// Destination exists; ensure it's a regular file before overwriting.
		if !info.Mode().IsRegular() {
			return fmt.Errorf("destination %q is not a regular file", dst)
		}
	} else if !os.IsNotExist(statErr) {
		// An unexpected error occurred while checking the destination.
		return statErr
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
