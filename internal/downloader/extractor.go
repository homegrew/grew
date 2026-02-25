package downloader

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/fsutil"
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
		if err := extractArchive(archivePath, destDir, spec.StripComponents); err != nil {
			return err
		}
		// If binary_name is set and the binary is at root (not in bin/), move it into bin/
		if spec.BinaryName != "" {
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

func extractArchive(archivePath, destDir string, stripComponents int) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(archivePath, destDir, stripComponents)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destDir, stripComponents)
	default:
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

// withinDir checks that target is inside destDir using absolute paths.
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
	return strings.HasPrefix(absTarget, absDir+string(filepath.Separator)) || absTarget == absDir
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

	tr := tar.NewReader(gz)
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

		target := filepath.Join(destDir, name)
		if !withinDir(destDir, target) {
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
			// Validate symlink target doesn't escape
			linkTarget := filepath.Join(filepath.Dir(target), header.Linkname)
			if !withinDir(destDir, linkTarget) {
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

		target := filepath.Join(destDir, name)
		if !withinDir(destDir, target) {
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

			// Validate symlink target doesn't escape. Resolve any existing
			// symlinks in the parent directory and the candidate target
			// path before checking that it remains within the extraction root.
			parentDir := filepath.Dir(target)
			realParentDir, err := filepath.EvalSymlinks(parentDir)
			if err != nil {
				// Cannot safely determine real parent; skip this entry.
				continue
			}
			candidateTarget := filepath.Join(realParentDir, linkTarget)
			realLinkTarget, err := filepath.EvalSymlinks(candidateTarget)
			if err != nil {
				// Target cannot be resolved safely; skip this entry.
				continue
			}
			if !withinDir(realDestDir, realLinkTarget) {
				// Resolved target escapes the extraction root; skip this entry.
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
