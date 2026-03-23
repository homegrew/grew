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

// mustBeWithin returns an error if target is not located within baseDir (or equal to it).
// Both paths are resolved to absolute, cleaned paths and compared with a path‑separator
// aware prefix check to avoid tricks like "/tmp/taps" vs "/tmp/taps2".
func mustBeWithin(baseDir, target string) error {
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolve base dir: %w", err)
	}
	baseAbs = filepath.Clean(baseAbs)

	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}
	targetAbs = filepath.Clean(targetAbs)

	if targetAbs == baseAbs {
		return nil
	}

	baseWithSep := baseAbs
	if !strings.HasSuffix(baseWithSep, string(os.PathSeparator)) {
		baseWithSep += string(os.PathSeparator)
	}
	if !strings.HasPrefix(targetAbs, baseWithSep) {
		return fmt.Errorf("path %q escapes base directory %q", targetAbs, baseAbs)
	}
	return nil
}

func Extract(archivePath, destDir string, spec formula.InstallSpec) error {
	archivePath = filepath.Clean(archivePath)
	destDir = filepath.Clean(destDir)
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve dest dir: %w", err)
	}
	destDir = absDest
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
			rootBin := filepath.Clean(filepath.Join(destDir, spec.BinaryName))
			if err := mustBeWithin(destDir, rootBin); err != nil {
				return fmt.Errorf("binary_name escapes destination directory: %w", err)
			}
			binDir := filepath.Clean(filepath.Join(destDir, "bin"))
			binDest := filepath.Clean(filepath.Join(binDir, spec.BinaryName))
			if err := mustBeWithin(destDir, binDest); err != nil {
				return fmt.Errorf("binary destination escapes destination directory: %w", err)
			}
			if info, err := os.Stat(rootBin); err == nil && !info.IsDir() {
				if binInfo, err := os.Stat(binDir); err != nil {
					if os.IsNotExist(err) {
						if err := os.MkdirAll(binDir, 0755); err != nil {
							return fmt.Errorf("create bin dir: %w", err)
						}
					} else {
						return fmt.Errorf("stat bin dir: %w", err)
					}
				} else if !binInfo.IsDir() {
					return fmt.Errorf("bin path %q exists but is not a directory", binDir)
				}

				if _, err := os.Stat(binDest); os.IsNotExist(err) {
					if err := os.Rename(rootBin, binDest); err != nil {
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
	srcPath = filepath.Clean(srcPath)
	destDir = filepath.Clean(destDir)
	if binaryName == "" {
		binaryName = filepath.Base(srcPath)
	}
	if err := validation.SafePathComponent(binaryName); err != nil {
		return fmt.Errorf("invalid binary name: %w", err)
	}
	binDir := filepath.Clean(filepath.Join(destDir, "bin"))
	if !withinDir(destDir, binDir) {
		return fmt.Errorf("bin directory escapes destination directory")
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	destPath := filepath.Clean(filepath.Join(binDir, binaryName))
	if !withinDir(destDir, destPath) {
		return fmt.Errorf("binary path escapes destination directory")
	}

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
	archivePath = filepath.Clean(archivePath)
	destDir = filepath.Clean(destDir)
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

// sanitizeSymlinkTarget validates and cleans a symlink target read from an
// archive entry. Returns the cleaned target or empty string if the target
// should be rejected. This ensures uncontrolled archive data is sanitized
// before it reaches any path expression or filesystem operation.
func sanitizeSymlinkTarget(target string) string {
	// Reject null bytes which cause discrepancies between Go path
	// functions (which process the full string) and OS syscalls
	// (which truncate at null).
	if strings.ContainsRune(target, 0) {
		return ""
	}
	// Reject absolute paths — they can point anywhere on the filesystem.
	if filepath.IsAbs(target) || strings.HasPrefix(target, "/") || strings.HasPrefix(target, `\`) {
		return ""
	}
	// Normalize the path to resolve redundant separators and "." components.
	clean := filepath.Clean(target)
	// Reject empty or no-op targets.
	if clean == "" || clean == "." {
		return ""
	}
	// Disallow any attempt to traverse upwards outside the extraction tree.
	// This covers patterns like "..", "../foo", "foo/../bar", "foo/..", etc.
	if clean == ".." ||
		strings.HasPrefix(clean, ".."+string(os.PathSeparator)) ||
		strings.Contains(clean, string(os.PathSeparator)+".."+string(os.PathSeparator)) ||
		strings.HasSuffix(clean, string(os.PathSeparator)+"..") {
		return ""
	}
	return clean
}

// safeJoinArchivePath joins a destination directory with a sanitized archive
// entry name. Returns the joined path only if it resolves within destDir.
// It performs three layers of defense:
//  1. Name sanitization — rejects "..", absolute paths, and other traversal patterns.
//  2. Relative-path check — filepath.Rel must produce a clean relative path
//     without leading ".." components (the standard Zip Slip guard).
//  3. Filesystem check — resolves symlinks in the parent directory to block
//     symlink indirection attacks (Zip Slip variant).
func safeJoinArchivePath(destDir, entryName string) (string, bool) {
	clean := sanitizeEntryName(entryName)
	if clean == "" {
		return "", false
	}
	target := filepath.Clean(filepath.Join(destDir, clean))

	// Standard Zip Slip check: the relative path from destDir to target
	// must not start with ".." or end with a trailing ".." segment after
	// filepath.Abs resolves both sides.
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return "", false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(absDestDir, absTarget)
	if err != nil ||
		rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		strings.HasSuffix(rel, string(filepath.Separator)+"..") {
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
			if info, err := os.Stat(target); err == nil {
				if !info.IsDir() {
					return fmt.Errorf("path %s exists and is not a directory", target)
				}
			} else if os.IsNotExist(err) {
				if err := os.MkdirAll(target, fsutil.SanitizeMode(os.FileMode(header.Mode), true)); err != nil {
					return fmt.Errorf("create directory %s: %w", target, err)
				}
			} else {
				return fmt.Errorf("stat directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent directory for %s: %w", target, err)
			}
			if err := extractFile(tr, target, fsutil.SanitizeMode(os.FileMode(header.Mode), false)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			linkname := sanitizeSymlinkTarget(header.Linkname)
			if linkname == "" {
				continue
			}
			// Validate that the symlink target, when interpreted relative to
			// the (possibly symlinked) parent directory, stays within destDir.
			// Resolve the parent directory to avoid following previously
			// extracted symlinks outside of destDir.
			parentDir := filepath.Dir(target)
			// As an extra safety check, ensure the parent directory itself
			// is within destDir before creating it.
			if !withinDir(destDir, parentDir) {
				continue
			}
			resolvedParent, err := filepath.EvalSymlinks(parentDir)
			if err != nil {
				// If the parent does not yet exist or cannot be resolved,
				// fall back to the intended parent path.
				resolvedParent = parentDir
			}
			resolved := filepath.Clean(filepath.Join(resolvedParent, linkname))
			realDest, err2 := filepath.EvalSymlinks(destDir)
			if err2 != nil {
				realDest = destDir
			}
			if !withinDir(realDest, resolved) {
				continue
			}
			if err := os.MkdirAll(parentDir, 0755); err != nil {
				return fmt.Errorf("create parent directory for symlink %s: %w", target, err)
			}
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove existing file before creating symlink %s: %w", target, err)
			}
			if err := os.Symlink(linkname, target); err != nil {
				return fmt.Errorf("create symlink %s -> %s: %w", target, linkname, err)
			}
		}
	}
	return nil
}

func extractTarGz(archivePath, destDir string, stripComponents int) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", archivePath, err)
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
		return fmt.Errorf("open output file %q: %w", path, err)
	}
	// Allow reading up to maxExtractSize+1 bytes so we can detect when more than
	// maxExtractSize bytes are available (n > maxExtractSize indicates overflow).
	lr := &io.LimitedReader{R: r, N: maxExtractSize + 1}
	n, err := io.Copy(out, lr)
	if err != nil {
		out.Close()
		return err
	}
	if n > maxExtractSize {
		// File exceeded the allowed size; remove partial output and return an error.
		out.Close()
		_ = os.Remove(path)
		return fmt.Errorf("extracted file %s exceeds maximum allowed size of %d bytes", path, maxExtractSize)
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

		// Join the archive entry name to the canonical destination directory
		// and ensure it cannot escape that root (including via symlinks).
		target, ok := safeJoinArchivePath(realDestDir, name)
		if !ok {
			continue
		}
		// Additional hardening: ensure the final target is still within the
		// canonical extraction root before using it.
		if !withinDir(realDestDir, target) {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, fsutil.SanitizeMode(f.Mode(), true)); err != nil {
				return err
			}
			continue
		}

		parentDir := filepath.Dir(target)
		// Guard against creating directories outside the extraction root.
		if !withinDir(realDestDir, parentDir) {
			return fmt.Errorf("refusing to create parent directory outside dest: %q", parentDir)
		}
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("create parent directory %q: %w", parentDir, err)
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q: %w", f.Name, err)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			buf := new(strings.Builder)
			_, err := io.Copy(buf, rc)
			if err != nil {
				rc.Close()
				return err
			}
			linkTarget := sanitizeSymlinkTarget(buf.String())
			if linkTarget == "" {
				rc.Close()
				continue
			}

			// Validate symlink target doesn't escape. Resolve any existing
			// symlinks in the parent directory and the candidate target
			// path before checking that it remains within the extraction root.
			parentDir := filepath.Dir(target)
			realParentDir, err := filepath.EvalSymlinks(parentDir)
			if err != nil {
				rc.Close()
				continue
			}
			candidateTarget := filepath.Join(realParentDir, linkTarget)
			realLinkTarget := candidateTarget
			if resolved, err := filepath.EvalSymlinks(candidateTarget); err == nil {
				// Successfully resolved the candidate target; use the resolved path.
				realLinkTarget = resolved
			} else if os.IsNotExist(err) {
				// Target does not exist; keep realLinkTarget as candidateTarget.
			} else {
				// For errors other than non-existent targets, skip creating the symlink.
				rc.Close()
				continue
			}
			if !withinDir(realDestDir, realLinkTarget) {
				rc.Close()
				continue
			}

			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				rc.Close()
				return err
			}
			if err := os.Symlink(linkTarget, target); err != nil {
				rc.Close()
				return err
			}
			rc.Close()
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
		src := filepath.Clean(filepath.Join(mountPoint, e.Name()))
		dst := filepath.Clean(filepath.Join(destDir, e.Name()))

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
	if archivePath == "" {
		return fmt.Errorf("archive path is empty")
	}

	absArchivePath, err := filepath.Abs(archivePath)
	if err != nil {
		return fmt.Errorf("failed to resolve archive path %q: %w", archivePath, err)
	}

	baseName := filepath.Base(absArchivePath)
	if err := validation.SafePathComponent(baseName); err != nil {
		return fmt.Errorf("unsafe archive filename %q (from %q): %w", baseName, absArchivePath, err)
	}

	lower := strings.ToLower(absArchivePath)
	var decompressCmd string
	switch {
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz"):
		decompressCmd = "xz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		decompressCmd = "bzip2"
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(absArchivePath))
	}

	if _, err := exec.LookPath(decompressCmd); err != nil {
		return fmt.Errorf("required decompression tool %q not found in PATH: %w", decompressCmd, err)
	}

	cmd := exec.Command(decompressCmd, "-d", "-c", "--", absArchivePath)
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
