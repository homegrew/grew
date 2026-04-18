// Package release provides helpers for downloading, verifying, and installing
// grew binary releases from GitHub. Used by both the getgrew tool and grew's
// self-update mechanism.
package release

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homegrew/grew/internal/downloader"
)

const (
	repoOwner = "homegrew"
	repoName  = "grew"

	// maxBinarySize limits the extracted binary to 128 MB.
	maxBinarySize = 128 << 20
)

var apiBase = "https://api.github.com"

func init() {
	if env := os.Getenv("HOMEGREW_GITHUB_API_BASE"); env != "" {
		apiBase = env
	}
}

// Release is the subset of the GitHub API release response we need.
type Release struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset represents a single file attached to a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// FetchLatest lists GitHub releases and returns the first stable (non-draft,
// non-prerelease) release.
func FetchLatest() (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=10", apiBase, repoOwner, repoName)

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

	for i := range releases {
		if releases[i].Draft || releases[i].Prerelease {
			continue
		}
		return &releases[i], nil
	}
	return nil, fmt.Errorf("no stable releases found for %s/%s", repoOwner, repoName)
}

// AssetName returns the expected tarball name for the current platform.
func AssetName() string {
	osName := runtime.GOOS
	archName := runtime.GOARCH
	switch osName {
	case "darwin":
		osName = "Darwin"
	case "linux":
		osName = "Linux"
	}
	switch archName {
	case "amd64":
		archName = "x86_64"
	}
	return fmt.Sprintf("grew_%s_%s.tar.gz", osName, archName)
}

// FindAssetURL returns the HTTPS download URL for the named asset in a release.
func FindAssetURL(rel *Release, name string) (string, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			if !strings.HasPrefix(a.BrowserDownloadURL, "https://") {
				return "", fmt.Errorf("asset %q has non-HTTPS URL: %s", name, a.BrowserDownloadURL)
			}
			return a.BrowserDownloadURL, nil
		}
	}
	available := make([]string, len(rel.Assets))
	for i, a := range rel.Assets {
		available[i] = a.Name
	}
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
			if len(hash) != 64 {
				return "", fmt.Errorf("invalid checksum length for %s: %d", assetName, len(hash))
			}
			return hash, nil
		}
	}
	return "", fmt.Errorf("no checksum found for %s in checksums.txt", assetName)
}

// DownloadBytes downloads a URL into memory. Limited to 1 MB.
func DownloadBytes(url string) ([]byte, error) {
	resp, err := httpsGet(url, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// DownloadTemp downloads a URL to a temporary file. Limited to 256 MB.
// The caller is responsible for removing the file.
func DownloadTemp(url string) (string, error) {
	resp, err := httpsGet(url, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}

	f, err := os.CreateTemp("", "grew-download-*")
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, io.LimitReader(resp.Body, 256<<20)); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// FileSHA256 computes the hex-encoded SHA256 of a file.
// Delegates to downloader.ComputeSHA256 to avoid duplicating the implementation.
func FileSHA256(path string) (string, error) {
	return downloader.ComputeSHA256(path)
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
	if !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("refusing non-HTTPS URL: %s", rawURL)
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
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
