package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/pkg/logger"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/validation"
)

func title(s string) string {
	if len(s) == 0 {
		return ""
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func main() {
	logger.Init(true, false) // Enable verbose output by default for the patcher tool.

	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <platform> <previous_release> <new_release>\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Example: patcher darwin/arm64 v0.4.0 v0.4.1")
		os.Exit(1)
	}

	platform := os.Args[1]
	prevRelease := os.Args[2]
	newRelease := os.Args[3]

	// Basic validation of version strings.
	// Since tags usually have a "v" prefix (like v0.4.0), we validate against IsValidVersion.
	if !validation.IsValidVersion(strings.TrimPrefix(prevRelease, "v")) || !validation.IsValidVersion(strings.TrimPrefix(newRelease, "v")) {
		slog.Error("Invalid version string provided.")
		os.Exit(1)
	}

	// Parse platform
	parts := strings.Split(platform, "/")
	if len(parts) != 2 {
		slog.Error("Invalid platform format. Use <os>/<arch> (e.g. darwin/arm64)")
		os.Exit(1)
	}
	osName := title(parts[0]) // Darwin, Linux
	arch := parts[1]          // x86_64, arm64

	projectName := "grew"
	if !validation.IsValidName(projectName) {
		slog.Error("Invalid project name configuration.")
		os.Exit(1)
	}

	// Check if bsdiff is available
	if _, err := exec.LookPath("bsdiff"); err != nil {
		slog.Error("bsdiff command not found. Please install bsdiff to generate patches.")
		os.Exit(1)
	}

	// Create a temp dir
	tmpDir, err := os.MkdirTemp("", "grew-patcher-*")
	if err != nil {
		slog.Error("Failed to create temp dir", "err", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	oldBin, err := safepath.SafeJoin(tmpDir, "old", projectName)
	if err != nil {
		slog.Error("Failed to create path for old binary", "err", err)
		os.Exit(1)
	}
	
	newBin, err := safepath.SafeJoin(tmpDir, "new", projectName)
	if err != nil {
		slog.Error("Failed to create path for new binary", "err", err)
		os.Exit(1)
	}

	fmt.Printf("==> Downloading %s %s for %s\n", projectName, prevRelease, platform)
	if err := downloadAndExtract(projectName, osName, arch, prevRelease, tmpDir, "old"); err != nil {
		slog.Error("Failed to get old release", "err", err)
		os.Exit(1)
	}

	fmt.Printf("==> Downloading %s %s for %s\n", projectName, newRelease, platform)
	if err := downloadAndExtract(projectName, osName, arch, newRelease, tmpDir, "new"); err != nil {
		slog.Error("Failed to get new release", "err", err)
		os.Exit(1)
	}

	patchFile := fmt.Sprintf("grew-%s-%s-%s-to-%s.bspatch", parts[0], parts[1], prevRelease, newRelease)
	fmt.Printf("==> Generating patch %s\n", patchFile)

	cmd := exec.Command("bsdiff", oldBin, newBin, patchFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		slog.Error("bsdiff failed", "err", err)
		os.Exit(1)
	}

	fmt.Printf("==> Success! Patch file created: %s\n", patchFile)
}

func downloadAndExtract(projectName, osName, arch, version, tmpDir, subDir string) error {
	filename := fmt.Sprintf("%s_%s_%s.tar.gz", projectName, osName, arch)
	url := fmt.Sprintf("https://github.com/homegrew/grew/releases/download/%s/%s", version, filename)

	dl := &downloader.Downloader{TmpDir: tmpDir}
	dlFile, err := dl.Download(url, fmt.Sprintf("%s_%s", subDir, filename))
	if err != nil {
		return fmt.Errorf("downloader error: %w", err)
	}

	destDir, err := safepath.SafeJoin(tmpDir, subDir)
	if err != nil {
		return fmt.Errorf("failed to create destination dir path: %w", err)
	}

	spec := formula.InstallSpec{
		Type:            "archive",
		StripComponents: 0,
		BinaryName:      projectName,
	}

	if err := downloader.Extract(dlFile, destDir, spec); err != nil {
		return fmt.Errorf("extraction error: %w", err)
	}

	// We rename it up to destDir
	binPath, err := safepath.SafeJoin(destDir, "bin", projectName)
	if err != nil {
		return fmt.Errorf("failed to resolve binpath: %w", err)
	}
	
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		binPath, err = safepath.SafeJoin(destDir, projectName)
		if err != nil {
			return fmt.Errorf("failed to resolve binpath in root: %w", err)
		}
	}

	finalPath, err := safepath.SafeJoin(destDir, projectName)
	if err != nil {
		return fmt.Errorf("failed to resolve finalpath: %w", err)
	}
	
	if binPath != finalPath {
		if err := os.Rename(binPath, finalPath); err != nil {
			return fmt.Errorf("failed to move extracted binary: %w", err)
		}
	}

	return nil
}
