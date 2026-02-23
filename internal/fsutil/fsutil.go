package fsutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyTree recursively copies a directory tree from src to dst.
// Symlinks are preserved but validated to not escape the destination.
func CopyTree(src, dst string) error {
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return fmt.Errorf("resolve dest: %w", err)
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// Detect symlinks via Lstat since Walk follows them
		linfo, lerr := os.Lstat(path)
		if lerr != nil {
			return lerr
		}

		if linfo.Mode()&os.ModeSymlink != 0 {
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
			if !strings.HasPrefix(linkAbs, absDst+string(filepath.Separator)) && linkAbs != absDst {
				// Skip symlinks that escape — don't fail, just skip silently.
				return nil
			}
			return os.Symlink(link, target)
		}

		if info.IsDir() {
			return os.MkdirAll(target, SanitizeMode(info.Mode(), true))
		}

		return CopyFile(path, target, SanitizeMode(info.Mode(), false))
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

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
