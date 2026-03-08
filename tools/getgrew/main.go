// Command getgrew downloads the latest grew binary release from GitHub,
// verifies its SHA256 checksum against the release's checksums.txt, and
// places the binary next to the getgrew executable. If that directory is
// not writable, it falls back to the current working directory.
// All downloads are HTTPS-only; redirects to HTTP are rejected.
//
// Install:
//
//	go install github.com/homegrew/grew/tools/getgrew@latest
//
// Usage:
//
//	getgrew              # download grew next to getgrew (or cwd as fallback)
//	getgrew -v           # verbose output (shows expected SHA256)
//	getgrew -debug       # debug output (implies -v; shows HTTP requests, redirects, temp paths)
//
// Then run ./grew setup to complete the installation.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/homegrew/grew/internal/release"
)

var (
	verbose bool
	debug   bool
)

func logf(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

func debugf(format string, args ...any) {
	if debug {
		fmt.Fprintf(os.Stderr, "[debug] "+format, args...)
	}
}

func main() {
	flag.BoolVar(&verbose, "v", false, "Verbose output")
	flag.BoolVar(&debug, "debug", false, "Debug output (implies verbose)")
	flag.Parse()

	if debug {
		verbose = true
	}

	destDir, err := resolveDestDir()
	if err != nil {
		log.Fatalf("failed to resolve destination directory: %v", err)
	}

	if err := run(destDir); err != nil {
		fmt.Fprintf(os.Stderr, "getgrew: %s\n", err)
		os.Exit(1)
	}
}

func resolveDestDir() (string, error) {
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if f, err := os.CreateTemp(exeDir, ".grew-probe-*"); err == nil {
			f.Close()
			os.Remove(f.Name())
			debugf("using executable directory: %s\n", exeDir)
			return exeDir, nil
		}
		debugf("executable directory %s is not writable, falling back to cwd\n", exeDir)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return cwd, nil
}

func run(destDir string) error {
	debugf("destDir=%s GOOS=%s GOARCH=%s\n", destDir, runtime.GOOS, runtime.GOARCH)

	rel, err := release.FetchLatest()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "==> Downloading grew %s for %s/%s\n", rel.TagName, runtime.GOOS, runtime.GOARCH)
	debugf("release tag=%s assets=%d\n", rel.TagName, len(rel.Assets))

	assetName := release.AssetName()
	debugf("asset name=%s\n", assetName)

	assetURL, err := release.FindAssetURL(rel, assetName)
	if err != nil {
		return err
	}
	debugf("asset URL=%s\n", assetURL)

	checksumURL, err := release.FindAssetURL(rel, "checksums.txt")
	if err != nil {
		return err
	}
	debugf("checksum URL=%s\n", checksumURL)

	fmt.Fprintf(os.Stderr, "==> Fetching checksums\n")
	checksums, err := release.DownloadBytes(checksumURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	debugf("checksums.txt:\n%s\n", checksums)

	expectedHash, err := release.FindChecksum(checksums, assetName)
	if err != nil {
		return err
	}
	logf("    Expected SHA256: %s\n", expectedHash)

	fmt.Fprintf(os.Stderr, "==> Downloading %s\n", assetName)
	tmpFile, err := release.DownloadTemp(assetURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer os.Remove(tmpFile)
	debugf("downloaded to temp: %s\n", tmpFile)

	actualHash, err := release.FileSHA256(tmpFile)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}
	debugf("actual SHA256: %s\n", actualHash)

	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch:\n  expected: %s\n  got:      %s", expectedHash, actualHash)
	}
	fmt.Fprintf(os.Stderr, "==> SHA256 verified: %s\n", actualHash)

	destPath := filepath.Join(destDir, "grew")
	debugf("extracting binary to %s\n", destPath)

	bin, err := release.ExtractBinaryFromFile(tmpFile)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	debugf("extracted binary: %d bytes\n", len(bin))

	if err := release.AtomicInstall(destPath, bin); err != nil {
		return fmt.Errorf("install: %w", err)
	}

	fmt.Fprintf(os.Stderr, "==> Saved to %s\n", destPath)
	fmt.Fprintf(os.Stderr, "\n    Run ./grew setup to complete the installation.\n")
	return nil
}
