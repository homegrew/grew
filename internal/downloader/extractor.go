package downloader

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/pkg/safepath"
)

func hasPathTraversal(rel string) bool {
	return rel == dotDot ||
		strings.HasPrefix(rel, dotDotWithSep) ||
		strings.HasSuffix(rel, sepWithDotDot)
}

// maxExtractSize limits individual file extraction to 512 MB.
const maxExtractSize = 512 << 20

// maxSymlinkTargetSize limits the maximum size of a symlink target we will read from an archive.
// Typical filesystems impose relatively small limits (often 4096 bytes), so this should be sufficient.
const maxSymlinkTargetSize int64 = 4096

// Common path components used for Zip Slip / path traversal checks.
const dotDot = ".."

var (
	dotDotWithSep = dotDot + string(filepath.Separator)
	sepWithDotDot = string(filepath.Separator) + dotDot
)

// mustBeWithin returns an error if target is not located within baseDir (or equal to it).
// Both paths are resolved to absolute, cleaned paths and compared with a path‑separator
// aware prefix check to avoid tricks like "/tmp/taps" vs "/tmp/taps2".

func resolveAndValidateExtractDest(destDir string) (string, error) {
	destDir = filepath.Clean(destDir)
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return "", fmt.Errorf("resolve dest dir: %w", err)
	}
	return filepath.Clean(absDest), nil
}

func Extract(archivePath, destDir string, spec formula.InstallSpec) error {
	archivePath = filepath.Clean(archivePath)
	absDest, err := resolveAndValidateExtractDest(destDir)
	if err != nil {
		return err
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
			return fmt.Errorf("extract archive %q to %q: %w", archivePath, destDir, err)
		}
		// If binary_name is set and the binary is at root (not in bin/), move it into bin/
		if spec.BinaryName != "" {
			if err := safepath.SafePathComponent(spec.BinaryName); err != nil {
				return fmt.Errorf("invalid binary_name: %w", err)
			}
			rootBin := filepath.Clean(filepath.Join(destDir, spec.BinaryName))
			if err := safepath.CheckSubpath(destDir, rootBin); err != nil {
				return fmt.Errorf("binary_name escapes destination directory: %w", err)
			}
			binDir := filepath.Clean(filepath.Join(destDir, "bin"))
			binDest := filepath.Clean(filepath.Join(binDir, spec.BinaryName))
			if err := safepath.CheckSubpath(destDir, binDest); err != nil {
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
	if err := safepath.SafePathComponent(binaryName); err != nil {
		return fmt.Errorf("invalid binary name: %w", err)
	}
	binDir := filepath.Clean(filepath.Join(destDir, "bin"))
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}
	destPath := filepath.Clean(filepath.Join(binDir, binaryName))
	if !safepath.IsSubpath(destDir, destPath) {
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
		if closeErr := dst.Close(); closeErr != nil {
			return fmt.Errorf("copy binary: %w; close dest binary: %v", err, closeErr)
		}
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

	// Prevent directory traversal attacks (Zip Slip) by strictly rejecting
	// any entry name containing ".." anywhere.
	if strings.Contains(name, "..") {
		return ""
	}

	// Normalize to forward slashes for consistent processing.
	slashed := filepath.ToSlash(name)

	// Clean the path to collapse things like "."
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
		strings.HasPrefix(clean, dotDotWithSep) ||
		strings.Contains(clean, sepWithDotDot) {
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
	if err != nil || hasPathTraversal(rel) {
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
		// We resolve destDir to its real path to ensure we're comparing
		// canonical paths. If resolution fails, we fall back to the raw destDir.
		realDest, err2 := filepath.EvalSymlinks(destDir)
		if err2 != nil {
			realDest = destDir
		}
		if !safepath.IsSubpath(realDest, realTarget) {
			return "", false
		}
	} else if !isNotExist(err) {
		// If EvalSymlinks failed for reasons other than NotExist, it might
		// be a real problem (e.g. permission denied).
		// We log this as a debug message and continue with the textual check
		// to be resilient against ephemeral filesystem states during extraction.
		slog.Debug(fmt.Sprintf("safeJoinArchivePath: EvalSymlinks(%q) failed: %v", parentDir, err))
	}

	// Final textual safety check: ensure the target is inside destDir.
	// This is our primary defense when the filesystem state is incomplete.
	// absDestDir and absTarget were already computed above.
	if !safepath.IsSubpath(absDestDir, absTarget) {
		return "", false
	}

	return target, true
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "not a directory") ||
		strings.Contains(msg, "file exists")
}

// extractSymlink safely creates a symlink if it doesn't escape the destination directory.
func extractSymlink(realDest, target, linkname string) error {
	slog.Debug(fmt.Sprintf("extractSymlink (v2) target: %s, linkname: %s", target, linkname))
	canonicalRealDest, err := filepath.Abs(realDest)
	if err != nil {
		return fmt.Errorf("resolve extraction destination %s: %w", realDest, err)
	}
	canonicalRealDest = filepath.Clean(canonicalRealDest)

	linkname = sanitizeSymlinkTarget(linkname)
	if linkname == "" {
		return nil
	}
	cleanLink := filepath.Clean(linkname)
	if filepath.IsAbs(cleanLink) || hasPathTraversal(cleanLink) {
		return nil
	}

	parentDir := filepath.Dir(target)
	if !safepath.IsSubpath(canonicalRealDest, parentDir) {
		return nil
	}
	resolvedParentDir, err := filepath.Abs(parentDir)
	if err != nil {
		return fmt.Errorf("resolve parent directory for symlink %s: %w", target, err)
	}
	resolvedParentDir = filepath.Clean(resolvedParentDir)
	if !safepath.IsSubpath(canonicalRealDest, resolvedParentDir) {
		return nil
	}

	// We use the parent directory of the symlink as the base for resolving its target.
	// We don't EvalSymlinks(parentDir) here because parentDir itself might be a
	// symlink that hasn't been fully resolved yet during extraction. Instead, we
	// trust safeJoinArchivePath to handle the safety checks.
	candidateTarget, ok := safeJoinArchivePath(parentDir, cleanLink)
	if !ok {
		return fmt.Errorf("couldn't resolve target symlink %s", target)
	}

	// Double-check safety of the candidate target.
	if !safepath.IsSubpath(canonicalRealDest, candidateTarget) {
		return nil
	}
	// Sink-side canonicalization and boundary check before any filesystem access.
	resolvedCandidateTarget, err := filepath.Abs(candidateTarget)
	if err != nil {
		return fmt.Errorf("resolve symlink candidate target %s: %w", candidateTarget, err)
	}
	resolvedCandidateTarget = filepath.Clean(resolvedCandidateTarget)
	if err := safepath.CheckSubpath(canonicalRealDest, resolvedCandidateTarget); err != nil {
		return nil
	}

	// If the target exists, ensure it doesn't resolve outside the root.
	// Re-check containment at sink to ensure the filesystem access path remains bounded.
	if err := safepath.CheckSubpath(canonicalRealDest, resolvedCandidateTarget); err != nil {
		return nil
	}
	if fi, err := os.Lstat(resolvedCandidateTarget); err == nil && fi != nil {
		if resolvedCandidate, err := filepath.EvalSymlinks(resolvedCandidateTarget); err == nil {
			if !safepath.IsSubpath(canonicalRealDest, resolvedCandidate) {
				return nil
			}
		}
	}

	// Final sink-side guard: canonicalize target and ensure it is still within extraction root.
	resolvedTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve symlink destination %s: %w", target, err)
	}
	resolvedTarget = filepath.Clean(resolvedTarget)
	if !safepath.IsSubpath(canonicalRealDest, resolvedTarget) {
		return nil
	}

	if err := os.RemoveAll(resolvedTarget); err != nil {
		return fmt.Errorf("remove existing path before creating symlink %s: %w", resolvedTarget, err)
	}
	if err := os.Symlink(linkname, resolvedTarget); err != nil {
		return fmt.Errorf("create symlink %q -> %q: %w", resolvedTarget, linkname, err)
	}
	return nil
}

// extractTar safely extracts entries from a tar stream with path traversal
// and symlink escape protection.
func extractTar(tr *tar.Reader, destDir string, stripComponents int) error {
	destDir = filepath.Clean(destDir)
	// Create destDir early to ensure we can resolve its symlinks consistently.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	realDest, err := filepath.EvalSymlinks(destDir)
	if err != nil {
		return fmt.Errorf("resolve destination directory: %w", err)
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		if strings.Contains(header.Name, "..") {
			continue
		}

		name := stripPath(header.Name, stripComponents)
		if name == "" {
			continue
		}

		target, ok := safeJoinArchivePath(realDest, name)
		if !ok {
			continue
		}
		targetDir := filepath.Dir(target)
		// Defensive sink-side validation: ensure target and its parent stay within
		// the resolved extraction root immediately before filesystem operations.
		if err := safepath.CheckSubpath(realDest, targetDir); err != nil {
			return fmt.Errorf("target parent directory escapes destination directory: %w", err)
		}
		if err := safepath.CheckSubpath(realDest, target); err != nil {
			return fmt.Errorf("target path escapes destination directory: %w", err)
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
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("create parent directory for %s: %w", target, err)
			}
			// Re-validate at sink: ensure the path to be removed/written is still
			// inside the resolved extraction destination.
			absTarget, err := filepath.Abs(target)
			if err != nil {
				return fmt.Errorf("resolve target path %s: %w", target, err)
			}
			absTarget = filepath.Clean(absTarget)
			if err := safepath.CheckSubpath(realDest, absTarget); err != nil {
				return fmt.Errorf("refuse to write outside extraction directory: %w", err)
			}

			// Re-anchor the sink path from trusted base + sanitized archive name.
			sinkTarget, err := safepath.SafeJoin(realDest, name)
			if err != nil {
				return fmt.Errorf("resolve sink target %q: %w", name, err)
			}
			sinkTarget = filepath.Clean(sinkTarget)
			if err := safepath.CheckSubpath(realDest, sinkTarget); err != nil {
				return fmt.Errorf("refuse to remove/write outside extraction directory: %w", err)
			}
			if sinkTarget != absTarget {
				return fmt.Errorf("target path mismatch after canonicalization: %q vs %q", absTarget, sinkTarget)
			}

			// Remove existing file/directory to avoid following symlinks or other conflicts.
			if err := os.RemoveAll(sinkTarget); err != nil {
				return fmt.Errorf("remove existing path %s: %w", sinkTarget, err)
			}
			if err := extractFile(tr, sinkTarget, fsutil.SanitizeMode(os.FileMode(header.Mode), false)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("create parent directory for symlink %s: %w", target, err)
			}
			if err := extractSymlink(realDest, target, header.Linkname); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				return fmt.Errorf("create parent directory for hard link %s: %w", target, err)
			}
			linkTarget, ok := safeJoinArchivePath(realDest, stripPath(header.Linkname, stripComponents))
			if !ok {
				continue
			}
			// Defensive sink-side validation: ensure both link source and destination
			// remain within the resolved extraction root.
			if err := safepath.CheckSubpath(realDest, linkTarget); err != nil {
				return fmt.Errorf("hard link source escapes destination directory: %w", err)
			}
			if err := safepath.CheckSubpath(realDest, target); err != nil {
				return fmt.Errorf("hard link destination escapes destination directory: %w", err)
			}
			// Re-validate immediately before destructive removal (defense in depth).
			if err := safepath.CheckSubpath(realDest, target); err != nil {
				return fmt.Errorf("refusing to remove hard link destination outside extraction root: %w", err)
			}
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("remove existing path %s for hard link: %w", target, err)
			}
			if err := os.Link(linkTarget, target); err != nil {
				return fmt.Errorf("create hard link %s -> %s: %w", target, linkTarget, err)
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

// extractFile writes a single file from a reader enforcing a maximum size.
// Files larger than maxExtractSize bytes cause an error, and any partially
// written output file is removed.
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
		if cerr := out.Close(); cerr != nil {
			return fmt.Errorf("copy data to %q: %v; additionally, closing output file %q failed: %w", path, err, path, cerr)
		}
		return fmt.Errorf("copy data to %q: %w", path, err)
	}
	if n > maxExtractSize {
		// File exceeded the allowed size; remove partial output and return an error.
		if cerr := out.Close(); cerr != nil {
			_ = os.Remove(path)
			return fmt.Errorf("extracted file %s exceeds maximum allowed size of %d bytes; additionally, closing output file failed: %w", path, maxExtractSize, cerr)
		}
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

	if absDest, err := filepath.Abs(destDir); err == nil {
		destDir = filepath.Clean(absDest)
	} else {
		destDir = filepath.Clean(destDir)
	}
	// Create destDir early to ensure we can resolve its symlinks consistently.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	// Resolve destination directory to its real path to ensure that
	// subsequent safety checks operate on canonical paths.
	realDestDir, err := filepath.EvalSymlinks(destDir)
	if err != nil {
		return fmt.Errorf("resolve destination directory: %w", err)
	}

	for _, f := range r.File {
		if strings.Contains(f.Name, "..") {
			continue
		}

		name := stripPath(f.Name, stripComponents)
		if name == "" {
			continue
		}
		// Explicitly reject unsafe archive entry paths (Zip Slip hardening):
		// - absolute paths
		// - null bytes
		// - traversal components (".."), after normalizing separators/cleaning
		normalizedName := filepath.Clean(filepath.FromSlash(name))
		if filepath.IsAbs(name) || strings.Contains(name, "\x00") || hasPathTraversal(normalizedName) {
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
		if !safepath.IsSubpath(realDestDir, target) {
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
		if !safepath.IsSubpath(realDestDir, parentDir) {
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
			buf := &strings.Builder{}
			// Limit the amount of data read for the symlink target to avoid excessive memory usage.
			_, err := io.Copy(buf, io.LimitReader(rc, maxSymlinkTargetSize))
			if err != nil {
				if cerr := rc.Close(); cerr != nil {
					return fmt.Errorf("copy symlink target for %q: %w; close reader: %v", f.Name, err, cerr)
				}
				return fmt.Errorf("copy symlink target for %q: %w", f.Name, err)
			}
			linkTarget := buf.String()
			rc.Close()
			if err := extractSymlink(realDestDir, target, linkTarget); err != nil {
				return err
			}
			continue
		}

		mode := fsutil.SanitizeMode(f.Mode(), false)

		// Final sink-time hardening: re-resolve the target from the trusted
		// extraction root and re-check containment immediately before
		// destructive/write operations.
		canonicalTarget, err := safepath.SafeJoin(realDestDir, name)
		if err != nil {
			rc.Close()
			return fmt.Errorf("refusing to resolve extraction target %q: %w", name, err)
		}
		if !safepath.IsSubpath(realDestDir, canonicalTarget) {
			rc.Close()
			return fmt.Errorf("refusing to write outside destination: %q", canonicalTarget)
		}

		if err := os.RemoveAll(canonicalTarget); err != nil {
			return fmt.Errorf("remove existing path %s: %w", canonicalTarget, err)
		}
		if err := extractFile(rc, canonicalTarget, mode); err != nil {
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
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if stderrStr != "" {
			return fmt.Errorf("mount DMG: %w (hdiutil stderr: %s)", err, stderrStr)
		}
		return fmt.Errorf("mount DMG: %w", err)
	}
	defer func() {
		if err := exec.Command("hdiutil", "detach", "-quiet", mountPoint).Run(); err != nil {
			slog.Warn(fmt.Sprintf("failed to detach DMG at %s: %v", mountPoint, err))
		}
	}()

	// Copy all top-level entries from the mounted volume to destDir.
	entries, err := os.ReadDir(mountPoint)
	if err != nil {
		return fmt.Errorf("read mounted DMG: %w", err)
	}
	for _, e := range entries {
		// Validate entry names from the mounted DMG to prevent traversal.
		if err := safepath.SafePathComponent(e.Name()); err != nil {
			continue
		}
		src := filepath.Clean(filepath.Join(mountPoint, e.Name()))
		dst := filepath.Clean(filepath.Join(destDir, e.Name()))

		// Verify constructed paths resolve within expected directories.
		if !safepath.IsSubpath(mountPoint, src) || !safepath.IsSubpath(destDir, dst) {
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
	if err := safepath.SafePathComponent(baseName); err != nil {
		return fmt.Errorf("unsafe archive filename %q (from %q): %w", baseName, absArchivePath, err)
	}

	// Ensure we use a normalized absolute path when invoking external tools.
	safeArchivePath := filepath.Clean(absArchivePath)

	lower := strings.ToLower(safeArchivePath)
	var decompressCmd string
	switch {
	case strings.HasSuffix(lower, ".tar.xz") || strings.HasSuffix(lower, ".txz"):
		decompressCmd = "xz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		decompressCmd = "bzip2"
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(safeArchivePath))
	}

	if _, err := exec.LookPath(decompressCmd); err != nil {
		return fmt.Errorf("required decompression tool %q not found in PATH: %w", decompressCmd, err)
	}

	cmd := exec.Command(decompressCmd, "-d", "-c", "--", safeArchivePath)
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
