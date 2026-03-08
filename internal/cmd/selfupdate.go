package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/homegrew/grew/internal/release"
)

func runSelfUpdate(_ []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	prefix := filepath.Dir(filepath.Dir(exePath))
	repoDir := filepath.Join(prefix, "Grew")
	destBin := filepath.Join(prefix, "bin", "grew")

	// Primary: git pull + go build.
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		fmt.Println("==> Updating grew from source...")
		if err := installFromGit(repoDir, destBin); err != nil {
			Logf("    Warning: source update failed: %v\n", err)
			fmt.Println("==> Falling back to latest release download...")
		} else {
			return nil
		}
	} else {
		Debugf("no git repo at %s, skipping source update\n", repoDir)
	}

	// Fallback: download latest release binary.
	return selfUpdateFromRelease(exePath)
}

// selfUpdateFromRelease downloads the latest stable release from GitHub,
// verifies its checksum, and replaces the running binary.
func selfUpdateFromRelease(exePath string) error {
	rel, err := release.FetchLatest()
	if err != nil {
		return err
	}
	fmt.Printf("==> Downloading grew %s for %s/%s\n", rel.TagName, runtime.GOOS, runtime.GOARCH)

	assetName := release.AssetName()
	Debugf("asset name: %s\n", assetName)

	assetURL, err := release.FindAssetURL(rel, assetName)
	if err != nil {
		return err
	}

	checksumURL, err := release.FindAssetURL(rel, "checksums.txt")
	if err != nil {
		return err
	}

	fmt.Println("==> Fetching checksums")
	checksums, err := release.DownloadBytes(checksumURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}

	expectedHash, err := release.FindChecksum(checksums, assetName)
	if err != nil {
		return err
	}
	Logf("    Expected SHA256: %s\n", expectedHash)

	fmt.Printf("==> Downloading %s\n", assetName)
	tmpFile, err := release.DownloadTemp(assetURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer os.Remove(tmpFile)

	actualHash, err := release.FileSHA256(tmpFile)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch:\n  expected: %s\n  got:      %s", expectedHash, actualHash)
	}
	fmt.Printf("==> SHA256 verified: %s\n", actualHash)

	bin, err := release.ExtractBinaryFromFile(tmpFile)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	Debugf("extracted binary: %d bytes\n", len(bin))

	if err := release.AtomicInstall(exePath, bin); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Printf("==> Updated to %s\n", rel.TagName)
	return nil
}
