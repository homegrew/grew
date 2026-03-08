package getgrew

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// maxBinarySize limits the extracted binary to 128 MB.
const maxBinarySize = 128 << 20

// extractGrew reads a .tar.gz stream and returns the contents of the "grew"
// binary found inside. It rejects path traversal, absolute paths, and
// symlinks. Only the first regular file named "grew" (at any depth) is returned.
func extractGrew(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}

		// Skip non-regular files (directories, symlinks, etc.).
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// Reject path traversal.
		clean := filepath.Clean(hdr.Name)
		if strings.Contains(clean, "..") || filepath.IsAbs(clean) {
			continue
		}

		// Match the "grew" binary at any nesting depth.
		if filepath.Base(clean) != "grew" {
			continue
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxBinarySize))
		if err != nil {
			return nil, fmt.Errorf("read binary from archive: %w", err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("binary \"grew\" not found in archive")
}
