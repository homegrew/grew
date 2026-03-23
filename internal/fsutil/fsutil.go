package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	const (
		isDirectory = true
		isFile      = false
	)

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
			linkAbs := link
			if !filepath.IsAbs(linkAbs) {
				linkAbs = filepath.Join(filepath.Dir(target), link)
			}
			linkAbs = filepath.Clean(linkAbs)
			if !isWithinRoot(absDst, linkAbs) {
				// Skip symlinks that escape — don't fail, just skip silently.
				return nil
			}
			return os.Symlink(link, target)
		}

		if info.IsDir() {
			dirMode := SanitizeMode(info.Mode(), isDirectory)
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

		return CopyFile(path, target, SanitizeMode(info.Mode(), isFile))
	})
}

// CopyFile copies a single file from src to dst.
func CopyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

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
			return 0755
		}
		return mode | 0700
	}
	if mode == 0 {
		return 0644
	}
	return mode
}
