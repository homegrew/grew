// Command installer downloads the latest grew binary release from GitHub,
// verifies its SHA256 checksum, and places it in the current directory.
//
// Install the installer:
//
//	go install github.com/homegrew/grew/cmd/installer@latest
//
// Usage:
//
//	installer              # download latest grew to ./grew
//	installer -d /tmp      # download to a specific directory
//	installer -v           # verbose output
//	installer -debug       # debug output (implies verbose)
//
// Then run ./grew setup to complete the installation.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	repoOwner = "homegrew"
	repoName  = "grew"
	apiBase   = "https://api.github.com"
)

var (
	verbose bool
	debug   bool
)

// logf prints to stderr when verbose (or debug) mode is enabled.
func logf(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

// debugf prints to stderr when debug mode is enabled, prefixed with [debug].
func debugf(format string, args ...any) {
	if debug {
		fmt.Fprintf(os.Stderr, "[debug] "+format, args...)
	}
}

func main() {
	destDir := flag.String("d", ".", "Directory to place the grew binary in")
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.BoolVar(&debug, "debug", false, "Debug output (implies verbose)")
	flag.Parse()

	if debug {
		verbose = true
	}

	if err := run(*destDir); err != nil {
		fmt.Fprintf(os.Stderr, "installer: %s\n", err)
		os.Exit(1)
	}
}

func run(destDir string) error {
	debugf("destDir=%s GOOS=%s GOARCH=%s\n", destDir, runtime.GOOS, runtime.GOARCH)

	// Resolve the latest release.
	rel, err := resolveRelease()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "==> Downloading grew %s for %s/%s\n", rel.TagName, runtime.GOOS, runtime.GOARCH)
	debugf("release tag=%s assets=%d\n", rel.TagName, len(rel.Assets))

	// Determine the expected asset name for this platform.
	assetName := binaryAssetName(rel.TagName)
	debugf("asset name=%s\n", assetName)

	assetURL, err := findAssetURL(rel, assetName)
	if err != nil {
		return err
	}
	debugf("asset URL=%s\n", assetURL)

	checksumURL, err := findAssetURL(rel, "checksums.txt")
	if err != nil {
		return err
	}
	debugf("checksum URL=%s\n", checksumURL)

	// Download and parse checksums.
	fmt.Fprintf(os.Stderr, "==> Fetching checksums\n")
	checksums, err := downloadToMemory(checksumURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	debugf("checksums.txt:\n%s\n", checksums)

	expectedHash, err := findChecksum(checksums, assetName)
	if err != nil {
		return err
	}
	logf("    Expected SHA256: %s\n", expectedHash)

	// Download the tarball to a temp file.
	fmt.Fprintf(os.Stderr, "==> Downloading %s\n", assetName)
	tmpFile, err := downloadToTemp(assetURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer os.Remove(tmpFile)
	debugf("downloaded to temp: %s\n", tmpFile)

	// Verify checksum of the downloaded archive.
	actualHash, err := fileSHA256(tmpFile)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}
	debugf("actual SHA256: %s\n", actualHash)

	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch:\n  expected: %s\n  got:      %s", expectedHash, actualHash)
	}
	fmt.Fprintf(os.Stderr, "==> SHA256 verified: %s\n", actualHash)

	// Extract and install.
	destPath := filepath.Join(destDir, "grew")
	debugf("extracting binary to %s\n", destPath)

	if err := installFile(tmpFile, destPath); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	fmt.Fprintf(os.Stderr, "==> Saved to %s\n", destPath)
	fmt.Fprintf(os.Stderr, "\n    Run ./grew setup to complete the installation.\n")

	return nil
}

// release is the subset of the GitHub API release response we need.
type release struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func resolveRelease() (*release, error) {
	// List releases sorted by creation date (newest first) and pick the first one.
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=10", apiBase, repoOwner, repoName)
	debugf("GET %s\n", url)

	resp, err := httpGetJSON(url)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()
	debugf("response: %s\n", resp.Status)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("parse releases JSON: %w", err)
	}

	// Skip drafts and prereleases.
	for i := range releases {
		if releases[i].Draft || releases[i].Prerelease {
			debugf("skipping %s (draft=%v prerelease=%v)\n", releases[i].TagName, releases[i].Draft, releases[i].Prerelease)
			continue
		}
		debugf("selected release: %s\n", releases[i].TagName)
		return &releases[i], nil
	}

	return nil, fmt.Errorf("no stable releases found for %s/%s", repoOwner, repoName)
}

// osName maps runtime.GOOS to the release asset OS name.
func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "Darwin"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

// archName maps runtime.GOARCH to the release asset architecture name.
func archName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

func binaryAssetName(_ string) string {
	return fmt.Sprintf("grew_%s_%s.tar.gz", osName(), archName())
}

func findAssetURL(rel *release, name string) (string, error) {
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

func findChecksum(data []byte, assetName string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: "<sha256>  <filename>" or "<sha256> <filename>"
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

func downloadToMemory(url string) ([]byte, error) {
	debugf("GET %s\n", url)
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	debugf("response: %s\n", resp.Status)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func downloadToTemp(url string) (string, error) {
	debugf("GET %s\n", url)
	resp, err := httpGet(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	debugf("response: %s content-length=%s\n", resp.Status, resp.Header.Get("Content-Length"))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s for %s", resp.Status, url)
	}

	f, err := os.CreateTemp("", "grew-installer-*")
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, io.LimitReader(resp.Body, 256<<20)); err != nil {
		os.Remove(f.Name())
		return "", err
	}

	if fi, err := f.Stat(); err == nil {
		debugf("downloaded %d bytes to %s\n", fi.Size(), f.Name())
	}

	return f.Name(), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func installFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	bin, err := extractGrew(srcF)
	if err != nil {
		return err
	}
	debugf("extracted binary: %d bytes\n", len(bin))

	// Atomic write: temp file in same directory, then rename.
	tmpDst, err := os.CreateTemp(filepath.Dir(dst), ".grew-install-*")
	if err != nil {
		return err
	}
	tmpPath := tmpDst.Name()
	debugf("writing to temp: %s\n", tmpPath)

	if _, err := tmpDst.Write(bin); err != nil {
		tmpDst.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmpDst.Chmod(0755); err != nil {
		tmpDst.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmpDst.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return err
	}
	debugf("renamed %s -> %s\n", tmpPath, dst)
	return nil
}

// httpGetJSON performs an HTTPS GET with the GitHub JSON API Accept header.
func httpGetJSON(rawURL string) (*http.Response, error) {
	return httpGetWithAccept(rawURL, "application/vnd.github+json")
}

func httpGet(rawURL string) (*http.Response, error) {
	return httpGetWithAccept(rawURL, "application/octet-stream")
}

func httpGetWithAccept(rawURL, accept string) (*http.Response, error) {
	if !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("refusing non-HTTPS URL: %s", rawURL)
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "grew-installer/1.0")
	req.Header.Set("Accept", accept)

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		debugf("using GITHUB_TOKEN for auth\n")
	}

	// Reject any redirect to non-HTTPS.
	client := *http.DefaultClient
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		debugf("redirect -> %s\n", req.URL)
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
