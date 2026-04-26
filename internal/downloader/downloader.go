package downloader

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/safepath"
)

// allowedHosts is the set of hosts that grew is permitted to download from.
// This prevents formula-supplied URLs from triggering requests to arbitrary
// internal services (SSRF). Extend via HOMEGREW_ALLOWED_HOSTS (comma-separated).
var allowedHosts = map[string]bool{
	"github.com":                    true,
	"objects.githubusercontent.com": true,
	"ghcr.io":                       true,
	"codeload.github.com":           true,
	"releases.hashicorp.com":        true,
	"downloads.sourceforge.net":     true,
	"storage.googleapis.com":        true,
	"dl.google.com":                 true,
	"ftp.gnu.org":                   true,
	"curl.se":                       true,
	"www.openssl.org":               true,
	"download.savannah.gnu.org":     true,
	"archive.mozilla.org":           true,
	"formulae.brew.sh":              true,
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
	safe, err := validateDownloadURL(rawURL)
	if err != nil {
		return "", err
	}

	// Validate filename to prevent path traversal (e.g. "../../etc/passwd").
	if err := safepath.SafePathComponent(filename); err != nil {
		return "", fmt.Errorf("invalid download filename: %w", err)
	}
	tmpDir := filepath.Clean(d.TmpDir)
	if abs, err := filepath.Abs(tmpDir); err == nil {
		tmpDir = filepath.Clean(abs)
	}
	destPath := filepath.Clean(filepath.Join(tmpDir, filename))
	if abs, err := filepath.Abs(destPath); err == nil {
		destPath = filepath.Clean(abs)
	}
	if !safepath.IsSubpath(tmpDir, destPath) {
		return "", fmt.Errorf("download path escapes temp directory")
	}
	// Resolve the final sink path via safe join right before filesystem use.
	sinkPath, err := safepath.SafeJoin(tmpDir, filename)
	if err != nil {
		return "", fmt.Errorf("download path escapes temp directory: %w", err)
	}
	// Defense-in-depth: enforce subpath constraint again at sink boundary.
	if err := safepath.CheckSubpath(tmpDir, sinkPath); err != nil {
		return "", fmt.Errorf("download path escapes temp directory: %w", err)
	}

	// Canonicalize paths (resolve symlinks) before filesystem operations.
	// This prevents writes outside tmpDir through symlinked parent directories.
	canonTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		return "", fmt.Errorf("resolve temp directory %s: %w", tmpDir, err)
	}
	canonTmpDir = filepath.Clean(canonTmpDir)

	sinkDir := filepath.Dir(sinkPath)
	canonSinkDir, err := filepath.EvalSymlinks(sinkDir)
	if err != nil {
		return "", fmt.Errorf("resolve download directory %s: %w", sinkDir, err)
	}
	canonSinkDir = filepath.Clean(canonSinkDir)
	if err := safepath.CheckSubpath(canonTmpDir, canonSinkDir); err != nil {
		return "", fmt.Errorf("download directory escapes temp directory: %w", err)
	}

	finalName := filepath.Base(sinkPath)
	if err := safepath.SafePathComponent(finalName); err != nil {
		return "", fmt.Errorf("invalid download filename: %w", err)
	}
	sinkPath, err = safepath.SafeJoin(canonTmpDir, finalName)
	if err != nil {
		return "", fmt.Errorf("download path escapes temp directory: %w", err)
	}
	if err := safepath.CheckSubpath(canonTmpDir, sinkPath); err != nil {
		return "", fmt.Errorf("download path escapes temp directory: %w", err)
	}

	// Final sink-adjacent validation: ensure the path is absolute and still
	// constrained to the canonical temp directory immediately before filesystem use.
	if err := safepath.SafeAbsolutePath(sinkPath); err != nil {
		return "", fmt.Errorf("invalid download path %q: %w", sinkPath, err)
	}
	if err := safepath.CheckSubpath(canonTmpDir, sinkPath); err != nil {
		return "", fmt.Errorf("download path escapes temp directory: %w", err)
	}

	// Build the request from the reconstructed url.URL, not the raw input.
	req := &http.Request{
		Method: "GET",
		URL:    safe,
		Header: make(http.Header),
	}
	// ghcr.io requires a bearer token for public OCI blob downloads.
	if safe.Host == "ghcr.io" {
		req.Header.Set("Authorization", "Bearer QQ==")
	}

	client := *http.DefaultClient
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to non-HTTPS URL: %s", r.URL)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d %s", rawURL, resp.StatusCode, resp.Status)
	}

	if fi, err := os.Lstat(sinkPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("prepare download path %s: refusing to overwrite symlink", sinkPath)
		}
		if fi.IsDir() {
			return "", fmt.Errorf("prepare download path %s: destination is a directory", sinkPath)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("prepare download path %s: %w", sinkPath, err)
	}

	out, err := os.OpenFile(sinkPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("create file %s: %w", sinkPath, err)
	}

	size := resp.ContentLength
	written, err := io.Copy(out, &progressReader{
		reader: resp.Body,
		total:  size,
		label:  filename,
	})
	cleanupSink := func() {
		// Recompute a trusted cleanup target from canonical tmp dir + validated final name.
		trustedSink, joinErr := safepath.SafeJoin(canonTmpDir, finalName)
		if joinErr != nil {
			return
		}
		trustedSink = filepath.Clean(trustedSink)
		if err := safepath.CheckSubpath(canonTmpDir, trustedSink); err != nil {
			return
		}

		// Ensure we only delete the file we intended to write.
		cleanSinkPath := filepath.Clean(sinkPath)
		if cleanSinkPath != trustedSink {
			return
		}

		_ = os.Remove(trustedSink)
	}
	if err != nil {
		_ = out.Close()
		cleanupSink()
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	if err := out.Close(); err != nil {
		cleanupSink()
		return "", fmt.Errorf("close file %s: %w", sinkPath, err)
	}

	fmt.Printf("\rDownloaded %s (%s)\n", filename, formatBytes(written))
	return sinkPath, nil
}

// validateDownloadURL parses and validates a URL for downloading.
// Returns a freshly constructed url.URL from validated components, breaking
// any taint chain from the original input string.
func validateDownloadURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid download URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("refusing to download over insecure scheme %q (only HTTPS is allowed)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("download URL has no host: %s", rawURL)
	}
	if !isHostAllowed(parsed.Host) {
		return nil, fmt.Errorf("download host %q is not in the allowed hosts list; "+
			"set HOMEGREW_ALLOWED_HOSTS=%s to allow it", parsed.Host, parsed.Hostname())
	}
	// Reconstruct the URL from validated components. This severs the data-flow
	// link between the raw user input and the URL used in the HTTP request.
	safe := &url.URL{
		Scheme:   "https",
		Host:     parsed.Host,
		Path:     parsed.Path,
		RawQuery: parsed.RawQuery,
		Fragment: parsed.Fragment,
	}
	return safe, nil
}

func computeHash(path string, h hash.Hash, algoName string) (string, error) {
	clean := filepath.Clean(path)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("invalid path for hashing")
	}
	if !filepath.IsAbs(clean) {
		abs, err := filepath.Abs(clean)
		if err != nil {
			return "", fmt.Errorf("invalid path for hashing")
		}
		clean = abs
	}

	f, err := os.Open(clean)
	if err != nil {
		return "", fmt.Errorf("open for hashing: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("compute %s: %w", algoName, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeSHA256 returns the hex-encoded SHA256 hash of a file.
func ComputeSHA256(path string) (string, error) {
	return computeHash(path, sha256.New(), "SHA256")
}

// ComputeSHA512 returns the hex-encoded SHA512 hash of a file.
func ComputeSHA512(path string) (string, error) {
	return computeHash(path, sha512.New(), "SHA512")
}

// ComputeSHA256Within returns the SHA256 of path after ensuring it stays within baseDir.
func ComputeSHA256Within(baseDir, path string) (string, error) {
	baseAbs, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", fmt.Errorf("invalid base directory for hashing")
	}
	pathAbs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("invalid path for hashing")
	}
	if err := safepath.CheckSubpath(baseAbs, pathAbs); err != nil {
		return "", fmt.Errorf("path for hashing escapes base directory: %w", err)
	}
	return ComputeSHA256(pathAbs)
}

// ComputeSHA512Within returns the SHA512 of path after ensuring it stays within baseDir.
func ComputeSHA512Within(baseDir, path string) (string, error) {
	baseAbs, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", fmt.Errorf("invalid base directory for hashing")
	}
	pathAbs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("invalid path for hashing")
	}
	if err := safepath.CheckSubpath(baseAbs, pathAbs); err != nil {
		return "", fmt.Errorf("path for hashing escapes base directory: %w", err)
	}
	return ComputeSHA512(pathAbs)
}

// VerifySHA256 checks that a file's SHA256 matches the expected hex string.
func VerifySHA256(path, expected string) error {
	actual, err := ComputeSHA256(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("SHA256 mismatch: expected %.16s..., got %.16s...", expected, actual)
	}
	return nil
}

// VerifySHA256Within checks SHA256 after constraining path to baseDir.
func VerifySHA256Within(baseDir, path, expected string) error {
	actual, err := ComputeSHA256Within(baseDir, path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("SHA256 mismatch: expected %.16s..., got %.16s...", expected, actual)
	}
	return nil
}

// VerifySHA512 checks that a file's SHA512 matches the expected hex string.
func VerifySHA512(path, expected string) error {
	actual, err := ComputeSHA512(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("SHA512 mismatch: expected %.16s..., got %.16s...", expected, actual)
	}
	return nil
}

// VerifySHA512Within checks SHA512 after constraining path to baseDir.
func VerifySHA512Within(baseDir, path, expected string) error {
	actual, err := ComputeSHA512Within(baseDir, path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("SHA512 mismatch: expected %.16s..., got %.16s...", expected, actual)
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
