package downloader

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/pkg/validation"
)

// maxExtractSize limits individual file extraction to 512 MB.
const maxExtractSize = 512 << 20

func Extract(archivePath, destDir string, spec formula.InstallSpec) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	switch spec.Type {
	case "binary":
		return installBinary(archivePath, destDir, spec.BinaryName)
	case "archive":
		if err := ExtractArchive(archivePath, destDir, spec.StripComponents); err != nil {
			return err
		}
		// If binary_name is set and the binary is at root (not in bin/), move it into bin/
		if spec.BinaryName != "" {
			if err := validation.SafePathComponent(spec.BinaryName); err != nil {
				return fmt.Errorf("invalid binary_name: %w", err)
			}
			rootBin := filepath.Join(destDir, spec.BinaryName)
			binDir := filepath.Join(destDir, "bin")
			if info, err := os.Stat(rootBin); err == nil && !info.IsDir() {
				if _, err := os.Stat(binDir); os.IsNotExist(err) {
					if err := os.MkdirAll(binDir, 0755); err != nil {
						return fmt.Errorf("create bin dir: %w", err)
					}
					if err := os.Rename(rootBin, filepath.Join(binDir, spec.BinaryName)); err != nil {
						return fmt.Errorf("move binary to bin/: %w", err)
					}
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown install type: %s", spec.Type)
	}
}

func installBinary(srcPath, destDir, binaryName string) error {
	if binaryName == "" {
		binaryName = filepath.Base(srcPath)
	}
	if err := validation.SafePathComponent(binaryName); err != nil {
		return fmt.Errorf("invalid binary name: %w", err)
	}
	binDir := filepath.Join(destDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	destPath := filepath.Join(binDir, binaryName)

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source binary: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("create dest binary: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("copy binary: %w", err)
	}
	return dst.Close()
}

func ExtractArchive(archivePath, destDir string, stripComponents int) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destDir, stripComponents)
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz") || strings.HasSuffix(lower, ".tar.bz2"):
		return extractTarXzBz2(archivePath, destDir, stripComponents)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destDir, stripComponents)
	case strings.HasSuffix(lower, ".dmg"):
		return extractDMG(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

// sanitizeEntryName cleans an archive entry name and rejects traversal attempts.
// Returns the cleaned name or empty string if the entry should be skipped.
func sanitizeEntryName(name string) string {
	if name == "" {
		return ""
	}

	// Normalize to forward slashes for consistent processing.
	slashed := filepath.ToSlash(name)

	// Quickly reject any explicit parent-directory components before cleaning.
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return ""
		}
	}

	// Clean the path to collapse things like "a/../b" and ".".
	clean := filepath.Clean(slashed)

	// Reject paths that clean to "." (current directory) or empty.
	if clean == "" || clean == "." {
		return ""
	}

	// Reject absolute or root-like paths. We check both IsAbs and a
	// leading slash to be defensive across platforms.
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, `\`) {
		return ""
	}

	// Reject any remaining ".." components after cleaning.
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return ""
		}
	}

	return clean
}

// safeJoinArchivePath joins a destination directory with a sanitized archive
// entry name. Returns the joined path only if it resolves within destDir.
// It performs both a textual check (against path traversal via "..") and a
// filesystem check (against symlink indirection created during extraction).
func safeJoinArchivePath(destDir, entryName string) (string, bool) {
	clean := sanitizeEntryName(entryName)
	if clean == "" {
		return "", false
	}
	target := filepath.Join(destDir, clean)
	if !withinDir(destDir, target) {
		return "", false
	}

	// Guard against Zip Slip via symlink indirection: a previous archive
	// entry may have created a symlink within destDir that points outside
	// it. Resolve the parent directory through the real filesystem and
	// verify the resolved target is still within destDir.
	parentDir := filepath.Dir(target)
	realParent, err := filepath.EvalSymlinks(parentDir)
	if err == nil {
		// Parent exists on disk — verify the resolved path stays inside.
		realTarget := filepath.Join(realParent, filepath.Base(target))
		realDest, err2 := filepath.EvalSymlinks(destDir)
		if err2 != nil {
			realDest = destDir
		}
		if !withinDir(realDest, realTarget) {
			return "", false
		}
	}
	// If parent doesn't exist yet (err != nil), the textual check is
	// sufficient — there are no symlinks to follow.

	return target, true
}

// withinDir checks that target is inside destDir using absolute, cleaned paths.
// This prevents path traversal attacks (e.g. "../../etc/passwd").
func withinDir(destDir, target string) bool {
	absDir, err := filepath.Abs(destDir)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	absDir = filepath.Clean(absDir)
	absTarget = filepath.Clean(absTarget)

	// Ensure absDir has a trailing separator when doing the prefix check
	// so that siblings with a common prefix are not matched.
	dirWithSep := absDir + string(filepath.Separator)
	return absTarget == absDir || strings.HasPrefix(absTarget, dirWithSep)
}

// extractTar safely extracts entries from a tar stream with path traversal
// and symlink escape protection.
func extractTar(tr *tar.Reader, destDir string, stripComponents int) error {
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		name := stripPath(header.Name, stripComponents)
		if name == "" {
			continue
		}

		target, ok := safeJoinArchivePath(destDir, name)
		if !ok {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, fsutil.SanitizeMode(os.FileMode(header.Mode), true)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := extractFile(tr, target, fsutil.SanitizeMode(os.FileMode(header.Mode), false)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Reject absolute symlink targets — they can point anywhere.
			if filepath.IsAbs(header.Linkname) {
				continue
			}
			// Validate the resolved symlink target stays within destDir,
			// resolving any existing symlinks along the path to prevent
			// escaping the extraction root via symlink chains.
			candidate := filepath.Join(filepath.Dir(target), header.Linkname)
			realTarget, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				// If the target cannot be safely resolved, skip this entry.
				continue
			}
			// Resolve destDir through symlinks too so both paths use the
			// same root (e.g. /var -> /private/var on macOS).
			realDestDir, err := filepath.EvalSymlinks(destDir)
			if err != nil {
				continue
			}
			if !withinDir(realDestDir, realTarget) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractTarGz(archivePath, destDir string, stripComponents int) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	return extractTar(tar.NewReader(gz), destDir, stripComponents)
}

// extractFile writes a single file from a reader with size limits.
func extractFile(r io.Reader, path string, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(r, maxExtractSize)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func extractZip(archivePath, destDir string, stripComponents int) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	// Resolve destination directory to its real path to ensure that
	// subsequent safety checks operate on canonical paths.
	realDestDir, err := filepath.EvalSymlinks(destDir)
	if err != nil {
		// If destDir does not exist yet or cannot be resolved, fall back
		// to its absolute path so we still have a consistent base.
		realDestDir, err = filepath.Abs(destDir)
		if err != nil {
			return err
		}
	}

	for _, f := range r.File {
		name := stripPath(f.Name, stripComponents)
		if name == "" {
			continue
		}

		target, ok := safeJoinArchivePath(destDir, name)
		if !ok {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, fsutil.SanitizeMode(f.Mode(), true)); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		if f.Mode()&os.ModeSymlink != 0 {
			buf := new(strings.Builder)
			_, err := io.Copy(buf, rc)
			rc.Close()
			if err != nil {
				return err
			}
			linkTarget := buf.String()

			// Reject absolute symlink targets — they can point anywhere.
			if filepath.IsAbs(linkTarget) {
				continue
			}

			// Validate symlink target doesn't escape. Resolve any existing
			// symlinks in the parent directory and the candidate target
			// path before checking that it remains within the extraction root.
			parentDir := filepath.Dir(target)
			realParentDir, err := filepath.EvalSymlinks(parentDir)
			if err != nil {
				continue
			}
			candidateTarget := filepath.Join(realParentDir, linkTarget)
			realLinkTarget, err := filepath.EvalSymlinks(candidateTarget)
			if err != nil {
				continue
			}
			if !withinDir(realDestDir, realLinkTarget) {
				continue
			}

			os.Remove(target)
			if err := os.Symlink(linkTarget, target); err != nil {
				return err
			}
			continue
		}

		mode := fsutil.SanitizeMode(f.Mode(), false)
		if err := extractFile(rc, target, mode); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return nil
}

// extractDMG mounts a macOS disk image, copies its contents to destDir, then detaches.
func extractDMG(dmgPath, destDir string) error {
	mountPoint, err := os.MkdirTemp("", "grew-dmg-*")
	if err != nil {
		return fmt.Errorf("create mount point: %w", err)
	}
	defer os.RemoveAll(mountPoint)

	// Mount the DMG read-only.
	cmd := exec.Command("hdiutil", "attach", "-nobrowse", "-noverify", "-readonly", "-mountpoint", mountPoint, dmgPath)
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mount DMG: %w", err)
	}
	defer exec.Command("hdiutil", "detach", "-quiet", mountPoint).Run()

	// Copy all top-level entries from the mounted volume to destDir.
	entries, err := os.ReadDir(mountPoint)
	if err != nil {
		return fmt.Errorf("read mounted DMG: %w", err)
	}
	for _, e := range entries {
		// Validate entry names from the mounted DMG to prevent traversal.
		if err := validation.SafePathComponent(e.Name()); err != nil {
			continue
		}
		src := filepath.Join(mountPoint, e.Name())
		dst := filepath.Join(destDir, e.Name())

		// Verify constructed paths resolve within expected directories.
		if !withinDir(mountPoint, src) || !withinDir(destDir, dst) {
			continue
		}

		// Use Lstat to verify actual file type — os.ReadDir may not
		// accurately report symlinks on all filesystems, and os.ReadFile
		// follows symlinks which could read files outside the mount.
		fi, err := os.Lstat(src)
		if err != nil {
			continue
		}

		switch {
		case fi.IsDir():
			if err := fsutil.CopyTree(src, dst); err != nil {
				return fmt.Errorf("copy %s from DMG: %w", e.Name(), err)
			}
		case fi.Mode().IsRegular():
			if err := fsutil.CopyFile(src, dst, fsutil.SanitizeMode(fi.Mode(), false)); err != nil {
				return fmt.Errorf("copy %s from DMG: %w", e.Name(), err)
			}
		}
		// Symlinks and other special files are silently skipped.
	}
	return nil
}

func stripPath(name string, strip int) string {
	if strip <= 0 {
		return name
	}
	parts := strings.SplitN(filepath.ToSlash(name), "/", strip+1)
	if len(parts) <= strip {
		return ""
	}
	return parts[strip]
}

// extractTarXzBz2 decompresses .tar.xz and .tar.bz2 archives using a system
// decompressor (xz or bzip2) and pipes the tar stream through the safe
// extractTar function, which validates all paths and symlinks.
func extractTarXzBz2(archivePath, destDir string, stripComponents int) error {
	lower := strings.ToLower(archivePath)
	var decompressCmd string
	switch {
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz"):
		decompressCmd = "xz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		decompressCmd = "bzip2"
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}

	cmd := exec.Command(decompressCmd, "-d", "-c", "--", archivePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("decompress %s: %w", decompressCmd, err)
	}

	extractErr := extractTar(tar.NewReader(stdout), destDir, stripComponents)

	if err := cmd.Wait(); err != nil && extractErr == nil {
		return fmt.Errorf("%s decompression failed: %w", decompressCmd, err)
	}
	return extractErr
}
