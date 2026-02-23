package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type Downloader struct {
	TmpDir string
}

func (d *Downloader) Download(url, filename string) (string, error) {
	destPath := filepath.Join(d.TmpDir, filename)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d %s", url, resp.StatusCode, resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer out.Close()

	size := resp.ContentLength
	written, err := io.Copy(out, &progressReader{
		reader: resp.Body,
		total:  size,
		label:  filename,
	})
	if err != nil {
		os.Remove(destPath)
		return "", fmt.Errorf("download %s: %w", url, err)
	}

	fmt.Printf("\rDownloaded %s (%s)\n", filename, formatBytes(written))
	return destPath, nil
}

func VerifySHA256(filepath, expected string) error {
	f, err := os.Open(filepath)
	if err != nil {
		return fmt.Errorf("open for verification: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("compute SHA256: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("SHA256 mismatch: expected %.16s..., got %.16s...", expected, actual)
	}
	return nil
}

type progressReader struct {
	reader  io.Reader
	total   int64
	current int64
	label   string
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.current += int64(n)
	if pr.total > 0 {
		pct := float64(pr.current) / float64(pr.total) * 100
		fmt.Printf("\rDownloading %s... %.1f%% (%s/%s)", pr.label, pct,
			formatBytes(pr.current), formatBytes(pr.total))
	} else {
		fmt.Printf("\rDownloading %s... %s", pr.label, formatBytes(pr.current))
	}
	return n, err
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
