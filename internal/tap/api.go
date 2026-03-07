package tap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/downloader"
)

const apiCommitsURL = "https://api.github.com/repos/homegrew/homegrew-taps/commits/main"

type githubCommit struct {
	SHA string `json:"sha"`
}

// UpdateAPI syncs the tap by downloading a tarball via the GitHub API,
// avoiding git overhead and providing better security by not trusting local .git state.
func (m *Manager) UpdateAPI() error {
	fmt.Println("==> Fetching latest tap SHA from GitHub API...")

	// 1. Get latest commit SHA securely
	req, err := http.NewRequest("GET", apiCommitsURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	// GitHub recommends sending a user agent
	req.Header.Set("User-Agent", "homegrew-cli")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch commit sha: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api returned %s", resp.Status)
	}

	var commit githubCommit
	if err := json.NewDecoder(resp.Body).Decode(&commit); err != nil {
		return fmt.Errorf("decode commit sha: %w", err)
	}

	if len(commit.SHA) < 40 {
		return fmt.Errorf("invalid commit sha received: %q", commit.SHA)
	}

	tarballURL := fmt.Sprintf("https://api.github.com/repos/homegrew/homegrew-taps/tarball/%s", commit.SHA)

	// 2. Setup tmp directory for download
	tmpDir, err := os.MkdirTemp("", "grew-taps-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 3. Download the tarball securely
	fmt.Printf("==> Downloading taps tarball (%s)...\n", commit.SHA[:7])
	dl := &downloader.Downloader{TmpDir: tmpDir}
	tarballPath, err := dl.Download(tarballURL, "taps.tar.gz")
	if err != nil {
		return fmt.Errorf("download tarball: %w", err)
	}

	// 4. Extract tarball securely to another temp directory
	extractDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("create extract dir: %w", err)
	}

	fmt.Println("==> Extracting taps...")
	// GitHub tarballs have a single root folder
	// stripComponents = 1 handles this securely, preventing ZipSlip.
	if err := downloader.ExtractArchive(tarballPath, extractDir, 1); err != nil {
		return fmt.Errorf("extract tarball: %w", err)
	}

	// 5. Replace old TapsDir with new one
	if err := os.RemoveAll(m.TapsDir); err != nil {
		return fmt.Errorf("remove old taps dir: %w", err)
	}

	// ensure parent dir exists before rename
	if err := os.MkdirAll(filepath.Dir(m.TapsDir), 0755); err != nil {
		return fmt.Errorf("create taps parent dir: %w", err)
	}

	if err := os.Rename(extractDir, m.TapsDir); err != nil {
		// Cross-device link fallback could be implemented if necessary,
		// but since tmpDir is usually on the same volume (if we set it correctly)
		// it might be an issue. Let's use standard os.Rename for now.
		return fmt.Errorf("rename extracted dir to taps dir: %w", err)
	}

	return nil
}
