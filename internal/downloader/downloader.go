package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// allowedHosts is the set of hosts that grew is permitted to download from.
// This prevents formula-supplied URLs from triggering requests to arbitrary
// internal services (SSRF). Extend via HOMEGREW_ALLOWED_HOSTS (comma-separated).
var allowedHosts = map[string]bool{
	"github.com":                true,
	"objects.githubusercontent.com": true,
	"ghcr.io":                  true,
	"api.github.com":           true,
	"codeload.github.com":      true,
	"releases.hashicorp.com":   true,
	"downloads.sourceforge.net": true,
	"storage.googleapis.com":   true,
	"dl.google.com":            true,
	"ftp.gnu.org":              true,
	"curl.se":                  true,
	"www.openssl.org":          true,
	"download.savannah.gnu.org": true,
	"archive.mozilla.org":      true,
	"formulae.brew.sh":         true,
}

func init() {
	if extra := os.Getenv("HOMEGREW_ALLOWED_HOSTS"); extra != "" {
		for _, h := range strings.Split(extra, ",") {
			h = strings.TrimSpace(h)
			if h != "" {
				allowedHosts[h] = true
			}
		}
	}
}

// isHostAllowed checks whether a host (with optional port) is in the allowlist.
func isHostAllowed(host string) bool {
	// Strip port if present (e.g. "example.com:443" → "example.com").
	h := host
	if idx := strings.LastIndex(h, ":"); idx != -1 {
		h = h[:idx]
	}
	if allowedHosts[h] {
		return true
	}
	// Allow subdomains of allowed hosts (e.g. "pkg.github.com" matches "github.com").
	for allowed := range allowedHosts {
		if strings.HasSuffix(h, "."+allowed) {
			return true
		}
	}
	return false
}

type Downloader struct {
	TmpDir string
}

// Download fetches a file over HTTPS from an allowed host.
// The URL is validated against a host allowlist to prevent SSRF.
// Extend the allowlist with HOMEGREW_ALLOWED_HOSTS=host1,host2,...
func (d *Downloader) Download(rawURL, filename string) (string, error) {
	safeURL, err := validateDownloadURL(rawURL)
	if err != nil {
		return "", err
	}

	destPath := filepath.Join(d.TmpDir, filename)

	req, err := http.NewRequest("GET", safeURL, nil)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	// ghcr.io requires a bearer token for public OCI blob downloads.
	if req.URL.Host == "ghcr.io" {
		req.Header.Set("Authorization", "Bearer QQ==")
	}

	resp, err := http.DefaultClient.Do(req) // #nosec — URL validated by validateDownloadURL
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d %s", rawURL, resp.StatusCode, resp.Status)
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
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}

	fmt.Printf("\rDownloaded %s (%s)\n", filename, formatBytes(written))
	return destPath, nil
}

// validateDownloadURL parses and validates a URL for downloading.
// Returns the sanitized URL string or an error.
func validateDownloadURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid download URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("refusing to download over insecure scheme %q (only HTTPS is allowed)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("download URL has no host: %s", rawURL)
	}
	if !isHostAllowed(parsed.Host) {
		return "", fmt.Errorf("download host %q is not in the allowed hosts list; "+
			"set HOMEGREW_ALLOWED_HOSTS=%s to allow it", parsed.Host, parsed.Hostname())
	}
	// Re-serialize to prevent URL injection via malformed components.
	return parsed.String(), nil
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
