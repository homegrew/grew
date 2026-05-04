// Package release provides helpers for downloading, verifying, and installing
// grew binary releases from GitHub. Used by both the grew setup command and
// grew's self-update mechanism.
package release

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
)

const (
	repoOwner = "homegrew"
	repoName  = "grew"

	// maxBinarySize limits the extracted binary to 128 MB.
	maxBinarySize = 128 << 20
)

var apiBase = "https://api.github.com"

// SetAPIBase overrides the GitHub API base URL.
// This is strictly intended for testing purposes and should only be called
// when runtime.DevMode is enabled. It validates the provided URL to prevent
// Server-Side Request Forgery (SSRF) vulnerabilities.
func SetAPIBase(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid api base url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("api base must use http or https")
	}
	// Restrict to known safe domains or local loopback for testing
	host := u.Hostname()
	if host != "api.github.com" && host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("api base host %q is not permitted", host)
	}
	apiBase = rawURL
	return nil
}

// Release is the subset of the GitHub API release response we need.
type Release struct {
	TagName    string                 `json:"tag_name"`
	Draft      bool                   `json:"draft"`
	Prerelease bool                   `json:"prerelease"`
	Assets     []Asset                `json:"assets"`
	DL         *downloader.Downloader `json:"-"`
}

// Asset represents a single file attached to a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchRelease tries to fetch the given release from Github.
// Returns a error if it fails.
func FetchRelease(s string) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", apiBase, repoOwner, repoName, s)

	resp, err := httpsGet(url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("fetch release %s: %w", s, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release %s not found", s)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse release %s: %w", s, err)
	}

	return &release, nil
}

// FetchRecent lists GitHub releases and returns the first 'count' stable
// (non-draft, non-prerelease) releases.
func FetchRecent(count int) ([]Release, error) {
	// Request more than count to account for drafts/prereleases.
	perPage := count + 5
	if perPage > 100 {
		perPage = 100
	}
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", apiBase, repoOwner, repoName, perPage)

	resp, err := httpsGet(url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var stable []Release
	for i := range releases {
		if releases[i].Draft || releases[i].Prerelease {
			continue
		}
		stable = append(stable, releases[i])
		if len(stable) >= count {
			break
		}
	}
	return stable, nil
}

// FetchLatest lists GitHub releases and returns the first stable (non-draft,
// non-prerelease) release.
func FetchLatest() (*Release, error) {
	stable, err := FetchRecent(1)
	if err != nil {
		return nil, err
	}
	if len(stable) == 0 {
		return nil, fmt.Errorf("no stable releases found for %s/%s", repoOwner, repoName)
	}
	return &stable[0], nil
}

func normalizePlatform() (osName, archName string) {
	osName = runtime.GOOS
	archName = runtime.GOARCH
	switch osName {
	case "darwin":
		osName = "Darwin"
	}
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	return
}

// AssetName returns the expected tarball name for the current platform.
func AssetName() string {
	osName, archName := normalizePlatform()
	return fmt.Sprintf("grew_%s_%s.tar.gz", osName, archName)
}

// RawBinaryName returns the expected uncompressed binary name for the current platform.
func RawBinaryName() string {
	osName, archName := normalizePlatform()
	return fmt.Sprintf("grew_%s_%s", osName, archName)
}

// PatchName returns the expected patch filename for the given version transition.
func PatchName(oldVer, newVer string) string {
	osName, archName := normalizePlatform()
	return fmt.Sprintf("grew_%s_%s_%s_to_%s.patch", osName, archName, oldVer, newVer)
}

// ParsePatchVersion extracts the 'old' version from a patch asset name if it
// matches the current platform's naming convention. Returns empty string if not a match.
func ParsePatchVersion(name string) string {
	osName, archName := normalizePlatform()
	prefix := fmt.Sprintf("grew_%s_%s_", osName, archName)
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".patch") {
		return ""
	}
	// "grew_Darwin_x86_64_v0.1.0_to_v0.2.0.patch" -> "v0.1.0_to_v0.2.0"
	rest := name[len(prefix) : len(name)-len(".patch")]
	parts := strings.Split(rest, "_to_")
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

// FindAssetURL returns the HTTPS download URL for the named asset in a release.
func FindAssetURL(rel *Release, name string) (string, error) {
	for _, a := range rel.Assets {
		slog.Info(fmt.Sprintf("Looking asset for %s", a.Name))
		if a.Name == name {
			if !strings.HasPrefix(a.BrowserDownloadURL, "https://") {
				return "", fmt.Errorf("asset %q has non-HTTPS URL: %s", name, a.BrowserDownloadURL)
			}
			slog.Debug(fmt.Sprintf("Found asset for %s", a.Name))
			return a.BrowserDownloadURL, nil
		}
	}

	available := make([]string, len(rel.Assets))
	for i, a := range rel.Assets {
		available[i] = a.Name
	}

	slog.Warn("No asset URL found for %s in release %s", name, rel.TagName)
	slog.Info(fmt.Sprintf("available: %s", strings.Join(available, ", ")))

	return "", fmt.Errorf("asset %q not found in release %s\n  Available: %s",
		name, rel.TagName, strings.Join(available, ", "))
}

// FindChecksum parses a checksums.txt file and returns the SHA256 hash
// for the given asset name.
func FindChecksum(data []byte, assetName string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if filepath.Base(parts[1]) == assetName {
			hash := strings.ToLower(parts[0])
			if len(hash) != 64 && len(hash) != 128 {
				return "", fmt.Errorf("invalid checksum length for %s: %d", assetName, len(hash))
			}
			return hash, nil
		}
	}
	return "", fmt.Errorf("no checksum found for %s in checksums.txt", assetName)
}

// FindChecksumBySize returns a checksum of the specified size (64 for SHA256, 128 for SHA512)
// for the given asset name from the provided checksum data.
func FindChecksumBySize(data []byte, assetName string, size int) (string, error) {
	hashes := FindAllChecksums(data, assetName)
	if h, ok := hashes[size]; ok {
		return h, nil
	}
	return "", fmt.Errorf("no checksum of size %d found for %s", size, assetName)
}

// FindAllChecksums parses a checksums.txt file and returns all hashes
// found for the given asset name, mapped by their length (64 for SHA256, 128 for SHA512).
func FindAllChecksums(data []byte, assetName string) map[int]string {
	results := make(map[int]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if filepath.Base(parts[1]) == assetName {
			hash := strings.ToLower(parts[0])
			if len(hash) == 64 || len(hash) == 128 {
				results[len(hash)] = hash
			}
		}
	}
	return results
}

// DownloadBytes downloads a URL into memory. Limited to 1 MB.
func (r *Release) DownloadBytes(url string) ([]byte, error) {
	dl := r.DL
	p := config.Default()
	if dl == nil {
		dl = &downloader.Downloader{TmpDir: p.Tmp}
	}
	return dl.DownloadBytes(url)
}

// DownloadTemp downloads a URL to a temporary file. Limited to 256 MB.
// If Release.DL is set and has CacheDir, it will use/populate the cache.
// The caller is responsible for removing the file if it's in a temporary location,
// but NOT if it's in the cache.
func (r *Release) DownloadTemp(url string, filename string) (string, error) {
	dl := r.DL
	p := config.Default()
	if dl == nil {
		dl = &downloader.Downloader{TmpDir: p.Tmp}
	}
	return dl.Download(url, filename)
}

// DownloadBytes downloads a URL into memory. Limited to 1 MB.
func DownloadBytes(url string) ([]byte, error) {
	dl := &downloader.Downloader{TmpDir: os.TempDir()}
	return dl.DownloadBytes(url)
}

// DownloadTemp downloads a URL to a temporary file. Limited to 256 MB.
// The caller is responsible for removing the file.
func DownloadTemp(url string) (string, error) {
	dl := &downloader.Downloader{TmpDir: os.TempDir()}
	return dl.Download(url, filepath.Base(url))
}

// FileSHA256 computes the hex-encoded SHA256 of a file.
// Delegates to downloader.ComputeSHA256 to avoid duplicating the implementation.
func FileSHA256(path string) (string, error) {
	return downloader.ComputeSHA256(path)
}

// FileSHA512 computes the hex-encoded SHA512 of a file.
// Delegates to downloader.ComputeSHA512 to avoid duplicating the implementation.
func FileSHA512(path string) (string, error) {
	return downloader.ComputeSHA512(path)
}

// ExtractBinary reads a .tar.gz and returns the contents of the "grew"
// binary. Rejects path traversal, absolute paths, and symlinks.
func ExtractBinary(r io.Reader) ([]byte, error) {
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
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		clean := filepath.Clean(hdr.Name)
		if strings.Contains(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
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

// ExtractBinaryFromFile opens a .tar.gz file and extracts the grew binary.
func ExtractBinaryFromFile(archivePath string) ([]byte, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ExtractBinary(f)
}

// AtomicInstall writes data to dst atomically (temp file + rename) with
// mode 0755.
func AtomicInstall(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".grew-install-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// httpsGet performs an HTTPS GET, rejecting non-HTTPS URLs and redirects.
func httpsGet(rawURL, accept string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	host := u.Hostname()
	isLocal := host == "localhost" || host == "127.0.0.1"

	if u.Scheme != "https" && !isLocal {
		return nil, fmt.Errorf("refusing non-HTTPS URL: %s", rawURL)
	}

	// Strictly validate the hostname to prevent SSRF vulnerabilities.
	switch host {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "127.0.0.1", "localhost":
		// allowed hosts
	default:
		return nil, fmt.Errorf("host %q is not permitted for release downloads", host)
	}

	// Reconstruct the URL from validated components.
	safe := &url.URL{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Path:     u.Path,
		RawQuery: u.RawQuery,
	}

	req := &http.Request{
		Method:     "GET",
		URL:        safe,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Host:       safe.Host,
	}

	req.Header.Set("User-Agent", "grew/1.0")
	req.Header.Set("Accept", accept)

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := *http.DefaultClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to non-HTTPS URL: %s", req.URL)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}

	return client.Do(req)
}
