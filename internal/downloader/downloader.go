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
	"sync"

	"github.com/homegrew/grew/internal/cache"
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
	"api.github.com":                true,
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

type DownloadRequest struct {
	URL            string
	Filename       string
	ExpectedSHA256 string
	ExpectedSHA512 string
}

type Downloader struct {
	TmpDir string
	Cache  *cache.Cache
	Silent bool
}

// Download fetches a file over HTTPS from an allowed host, using Cache if available.
// If the file already exists in Cache, it returns the path to the cached file.
// Otherwise, it downloads the file to Cache and returns the path.
// The URL is validated against a host allowlist to prevent SSRF.
func (d *Downloader) Download(rawURL, filename string) (string, error) {
	safe, err := validateDownloadURL(rawURL)
	if err != nil {
		return "", err
	}

	// Validate filename to prevent path traversal.
	if err := safepath.SafePathComponent(filename); err != nil {
		return "", fmt.Errorf("invalid download filename: %w", err)
	}

	// If Cache is set, try to use/populate the cache.
	if d.Cache != nil {
		if d.Cache.Exists(filename) {
			fmt.Printf("Using cached %s\n", filename)
			return d.Cache.DownloadPath(filename)
		}

		// Download to a temporary file first.
		tmpFile, err := d.downloadToTmp(safe, filename)
		if err != nil {
			return "", err
		}

		// Move from tmp to cache.
		path, err := d.Cache.Store(tmpFile, filename)
		if err != nil {
			// Sink-adjacent guard: only remove files that are confirmed to be
			// within the temporary directory that actually contains tmpFile.
			cleanupBase := filepath.Clean(filepath.Dir(tmpFile))
			if abs, aerr := filepath.Abs(cleanupBase); aerr == nil {
				cleanupBase = filepath.Clean(abs)
			}
			if eval, eerr := filepath.EvalSymlinks(cleanupBase); eerr == nil {
				cleanupBase = filepath.Clean(eval)
			}
			if serr := safepath.SafeAbsolutePath(cleanupBase); serr == nil {
				// Rebuild a trusted cleanup path from canonical base + validated basename.
				tmpName := filepath.Base(tmpFile)
				if ferr := safepath.SafePathComponent(tmpName); ferr == nil {
					if safeCleanupPath, jerr := safepath.SafeJoin(cleanupBase, tmpName); jerr == nil &&
						safepath.SafeAbsolutePath(safeCleanupPath) == nil &&
						safepath.CheckSubpath(cleanupBase, safeCleanupPath) == nil {
						_ = os.Remove(safeCleanupPath)
					}
				}
			}
			return "", err
		}
		return path, nil
	}

	// No cache, download directly to TmpDir (original behavior).
	return d.downloadToTmp(safe, filename)
}

// DownloadBytes fetches a URL and returns its content as a byte slice.
// It does NOT use CacheDir as it is intended for small files like checksums.
// Limited to 1 MB.
func (d *Downloader) DownloadBytes(rawURL string) ([]byte, error) {
	safe, err := validateDownloadURL(rawURL)
	if err != nil {
		return nil, err
	}

	req := &http.Request{
		Method: "GET",
		URL:    safe,
		Header: make(http.Header),
	}
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d %s", rawURL, resp.StatusCode, resp.Status)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// BatchDownload downloads multiple files concurrently using a worker pool.
func (d *Downloader) BatchDownload(downloads []DownloadRequest, maxWorkers int) error {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}

	silentD := *d
	silentD.Silent = true

	reqCh := make(chan DownloadRequest, len(downloads))
	errCh := make(chan error, len(downloads))
	var wg sync.WaitGroup

	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for req := range reqCh {
				path, err := silentD.Download(req.URL, req.Filename)
				if err != nil {
					errCh <- fmt.Errorf("download %s failed: %w", req.Filename, err)
					continue
				}
				if req.ExpectedSHA256 != "" {
					if err := VerifySHA256(path, req.ExpectedSHA256); err != nil {
						_ = os.Remove(path)
						errCh <- fmt.Errorf("verify %s failed: %w", req.Filename, err)
						continue
					}
				}
				if req.ExpectedSHA512 != "" {
					if err := VerifySHA512(path, req.ExpectedSHA512); err != nil {
						_ = os.Remove(path)
						errCh <- fmt.Errorf("verify %s failed: %w", req.Filename, err)
						continue
					}
				}
				errCh <- nil
			}
		}()
	}

	for _, req := range downloads {
		reqCh <- req
	}
	close(reqCh)

	wg.Wait()
	close(errCh)

	var errs []string
	for err := range errCh {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("batch download failed: %s", strings.Join(errs, "; "))
	}

	return nil
}

func (d *Downloader) downloadToTmp(safeURL *url.URL, filename string) (string, error) {
	tmpDir := filepath.Clean(d.TmpDir)
	if abs, err := filepath.Abs(tmpDir); err == nil {
		tmpDir = filepath.Clean(abs)
	}
	// On some platforms (macOS), /var is a symlink to /private/var.
	// We must resolve symlinks before performing subpath checks.
	if eval, err := filepath.EvalSymlinks(tmpDir); err == nil {
		tmpDir = filepath.Clean(eval)
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
	canonTmpDir := tmpDir
	if err := safepath.SafeAbsolutePath(canonTmpDir); err != nil {
		return "", fmt.Errorf("invalid temp directory %q: %w", canonTmpDir, err)
	}

	finalName := filepath.Base(sinkPath)
	if err := safepath.SafePathComponent(finalName); err != nil {
		return "", fmt.Errorf("invalid download filename: %w", err)
	}

	// Final sink-adjacent validation: rebuild and validate the path that will be
	// passed to filesystem APIs, using only canonical trusted base + validated name.
	safeSinkPath, err := safepath.SafeJoin(canonTmpDir, finalName)
	if err != nil {
		return "", fmt.Errorf("download path escapes temp directory: %w", err)
	}
	if err := safepath.CheckSubpath(canonTmpDir, safeSinkPath); err != nil {
		return "", fmt.Errorf("download path escapes temp directory: %w", err)
	}

	// Build the request from the reconstructed url.URL, not the raw input.
	req := &http.Request{
		Method: "GET",
		URL:    safeURL,
		Header: make(http.Header),
	}
	// ghcr.io requires a bearer token for public OCI blob downloads.
	if safeURL.Host == "ghcr.io" {
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
		return "", fmt.Errorf("download %s: %w", safeURL.String(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d %s", safeURL.String(), resp.StatusCode, resp.Status)
	}

	if fi, err := os.Lstat(safeSinkPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("prepare download path %s: refusing to overwrite symlink", safeSinkPath)
		}
		if fi.IsDir() {
			return "", fmt.Errorf("prepare download path %s: destination is a directory", safeSinkPath)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("prepare download path %s: %w", safeSinkPath, err)
	}

	tmpFile, err := os.CreateTemp(canonTmpDir, "grew-dl-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpFilePath := tmpFile.Name()

	size := resp.ContentLength
	var bodyReader io.Reader = resp.Body
	if !d.Silent {
		bodyReader = &progressReader{
			reader: resp.Body,
			total:  size,
			label:  filename,
		}
	} else {
		fmt.Printf("Downloading %s...\n", filename)
	}

	written, err := io.Copy(tmpFile, bodyReader)
	
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFilePath)
		return "", fmt.Errorf("download %s: %w", safeURL.String(), err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFilePath)
		return "", fmt.Errorf("close temp file %s: %w", tmpFilePath, err)
	}

	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			_ = os.Chtimes(tmpFilePath, t, t)
		}
	}

	finalPath := tmpFilePath
	if d.Cache == nil {
		// Final sink-time guard: both source and destination must remain within
		// the canonical temp directory before performing filesystem rename.
		if err := safepath.CheckSubpath(canonTmpDir, tmpFilePath); err != nil {
			_ = os.Remove(tmpFilePath)
			return "", fmt.Errorf("temporary file escaped temp directory: %w", err)
		}
		if err := safepath.CheckSubpath(canonTmpDir, safeSinkPath); err != nil {
			_ = os.Remove(tmpFilePath)
			return "", fmt.Errorf("download sink escaped temp directory: %w", err)
		}
		if err := os.Rename(tmpFilePath, safeSinkPath); err != nil {
			_ = os.Remove(tmpFilePath)
			return "", fmt.Errorf("rename temp file to %s: %w", safeSinkPath, err)
		}
		finalPath = safeSinkPath
	}

	if !d.Silent {
		fmt.Printf("\rDownloaded %s (%s)\n", filename, formatBytes(written))
	} else {
		fmt.Printf("Downloaded %s (%s)\n", filename, formatBytes(written))
	}
	return finalPath, nil
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
