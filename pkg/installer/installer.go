package installer

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"runtime"

	"github.com/homegrew/grew/pkg/auditlog"
	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/downloader"
	"github.com/homegrew/grew/pkg/release"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/sandbox"
	"github.com/homegrew/grew/pkg/ui"
	verpkg "github.com/homegrew/grew/pkg/version"
)

var ErrNoGitRepo = errors.New("no git repository found")

// installFromGit clones the grew repository and builds the binary from source.
// If the repo already exists, it pulls the latest changes instead.
func InstallFromGit(repoURL, repoDir, destBin string, allowClone bool) error {
	if err := safepath.SafeAbsolutePath(repoDir); err != nil {
		return fmt.Errorf("invalid repository directory: %w", err)
	}
	if err := safepath.SafeAbsolutePath(destBin); err != nil {
		return fmt.Errorf("invalid destination binary path: %w", err)
	}

	cleanRepoDir := repoDir // already validated as clean

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git not found in PATH")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("go not found in PATH")
	}

	gitDir, err := safepath.SafeJoin(cleanRepoDir, ".git")
	if err != nil {
		return fmt.Errorf("construct git directory path: %w", err)
	}
	if _, err := os.Stat(gitDir); err == nil {
		// Repo exists — pull latest.
		fmt.Fprintln(os.Stderr, "==> Updating grew source...")
		pull := exec.Command(gitPath, "pull", "--ff-only")
		pull.Dir = cleanRepoDir
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		if err := pull.Run(); err != nil {
			return fmt.Errorf("git pull: %w", err)
		}
	} else {
		if !allowClone {
			return ErrNoGitRepo
		}
		// Clone fresh.
		ui.FprintArrow(os.Stderr, "Cloning grew from %s", repoURL)
		clone := exec.Command(gitPath, "clone", "--depth", "1", "--", repoURL, cleanRepoDir)
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
	}

	// Generate version and build.
	fmt.Fprintln(os.Stderr, "==> Building grew from source...")
	build := exec.Command(goPath, "build", "-o", destBin, "-trimpath", "-ldflags", fmt.Sprintf("-s -w -X main.buildVersion=%s", verpkg.Version()), ".")
	build.Dir = cleanRepoDir
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	ui.FprintArrow(os.Stderr, "Built and installed grew to %s", destBin)
	return nil
}

// CheckOSVForVersion queries OSV.dev for known vulnerabilities affecting the specified version.

// VerifyBinaryIntegrity runs a basic check on the binary.
func VerifyBinaryIntegrity(binPath string, expectedVersion string) error {
	if err := safepath.SafeAbsolutePath(binPath); err != nil {
		return fmt.Errorf("invalid binary path: %w", err)
	}

	slog.Debug("verifying binary integrity", "path", binPath)
	info, err := os.Stat(binPath)
	if err != nil {
		return fmt.Errorf("binary does not exist: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("binary is not a regular file")
	}

	slog.Debug("verifying binary integrity", "path", binPath)
	cmd := exec.Command(binPath, "--", "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute binary check: %w\noutput: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return fmt.Errorf("no version output")
	}

	slog.Debug(fmt.Sprintf("binary version output: %s", outStr))
	slog.Debug(fmt.Sprintf("expected version: %s", expectedVersion))
	slog.Debug("checking if expected version is contained in output or if output contains 'dev'")
	if expectedVersion != "" && !strings.Contains(outStr, expectedVersion) && !strings.Contains(outStr, "dev") {
		return fmt.Errorf("version mismatch: expected %s, got %s", expectedVersion, outStr)
	}

	slog.Debug("binary integrity check passed")
	return nil
}

// FileHashes returns the hex-encoded SHA256 and SHA512 hashes of a file.
func FileHashes(path string) (string, string, error) {
	return downloader.ComputeHashes(path)
}

func InstallLatestRelease(exePath string, rel *release.Release) error {
	// Apply OSV security gate before full download.
	targetVer := rel.TagName
	if res, err := CheckOSVForVersion("github.com/homegrew/grew", targetVer); err != nil {
		slog.Warn(fmt.Sprintf("OSV query failed (proceeding): %v", err))
	} else if res.Vulnerable {
		return fmt.Errorf("target version %s is vulnerable: %s", targetVer, res.Message)
	}

	ui.FprintArrow(os.Stderr, "Downloading grew %s for %s/%s", rel.TagName, runtime.GOOS, runtime.GOARCH)

	assetName := release.AssetName()
	slog.Debug("asset name: " + assetName)

	if err := safepath.SafePathComponent(assetName); err != nil {
		return fmt.Errorf("invalid asset name %q: %w", assetName, err)
	}

	assetURL, err := release.FindAssetURL(rel, assetName)
	if err != nil {
		return err
	}

	checksumURL, err := release.FindAssetURL(rel, "checksums.txt")
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "==> Fetching checksums")
	checksums, err := rel.DownloadBytes(checksumURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}

	expectedHashes := release.FindAllChecksums(checksums, assetName)
	if len(expectedHashes) == 0 {
		return fmt.Errorf("no checksum found for %s in checksums.txt", assetName)
	}
	for length, hash := range expectedHashes {
		algo := "SHA-256"
		if length == 128 {
			algo = "SHA-512"
		}
		slog.Info(fmt.Sprintf("expected %s: %s", algo, hash))
	}

	ui.FprintArrow(os.Stderr, "Downloading %s", assetName)
	tmpFile, err := rel.DownloadTemp(assetURL, assetName)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Only remove if it's NOT in the cache.
	// Canonicalize tmpFile and its expected base directory before removal for security hardening.
	inCache := false
	if rel.DL != nil && rel.DL.Cache != nil {
		cacheDir := filepath.Clean(rel.DL.Cache.Dir())
		if eval, err := filepath.EvalSymlinks(cacheDir); err == nil {
			cacheDir = filepath.Clean(eval)
		}
		candidate := filepath.Clean(tmpFile)
		if eval, err := filepath.EvalSymlinks(candidate); err == nil {
			candidate = filepath.Clean(eval)
		}
		inCache = safepath.CheckSubpath(cacheDir, candidate) == nil
	}
	if !inCache {
		cleanedTmpFile := filepath.Clean(tmpFile)
		if eval, err := filepath.EvalSymlinks(cleanedTmpFile); err == nil {
			cleanedTmpFile = filepath.Clean(eval)
		}

		// Determine a trusted temporary directory for cleanup authorization.
		// Always anchor deletion checks to the system temp directory to avoid
		// using potentially user-influenced configured paths.
		expectedTmpDir := os.TempDir()

		// Canonicalize the trusted temporary directory path.
		if eval, err := filepath.EvalSymlinks(expectedTmpDir); err == nil {
			expectedTmpDir = filepath.Clean(eval)
		} else {
			expectedTmpDir = filepath.Clean(expectedTmpDir)
		}

		// Ensure the file to be removed is strictly within the trusted system temp directory.
		if err := safepath.CheckSubpath(expectedTmpDir, cleanedTmpFile); err != nil {
			slog.Error(fmt.Sprintf("security: refusing to remove temporary file %q outside trusted temp directory %q: %v", cleanedTmpFile, expectedTmpDir, err))
			return fmt.Errorf("temporary file %q escaped trusted temporary directory: %w", cleanedTmpFile, err)
		}
		defer func() {
			if err := os.Remove(cleanedTmpFile); err != nil {
				slog.Debug("failed to remove temporary file", "file", cleanedTmpFile, "error", err)
			}
		}()
	}

	// Verify all available hashes.
	sha256Actual, sha512Actual, err := FileHashes(tmpFile)
	if err != nil {
		return fmt.Errorf("hash downloaded file: %w", err)
	}

	if expected, ok := expectedHashes[64]; ok {
		if sha256Actual != expected {
			return fmt.Errorf("SHA-256 mismatch: got %s, want %s", sha256Actual, expected)
		}
		ui.FprintArrow(os.Stderr, "SHA-256 verified: %s", sha256Actual)
	}
	if expected, ok := expectedHashes[128]; ok {
		if sha512Actual != expected {
			return fmt.Errorf("SHA-512 mismatch: got %s, want %s", sha512Actual, expected)
		}
		ui.FprintArrow(os.Stderr, "SHA-512 verified: %s", sha512Actual)
	}

	bin, err := release.ExtractBinaryFromFile(tmpFile)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	slog.Debug(fmt.Sprintf("extracted binary: %d bytes", len(bin)))

	// Health Check: run vuln-scan on new binary (sandboxed) before installation.
	healthDir, err := os.MkdirTemp("", "grew-health-*")
	if err != nil {
		return fmt.Errorf("create health check tmpdir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(healthDir); err != nil {
			slog.Debug("failed to remove health check directory", "directory", healthDir, "error", err)
		}
	}()
	healthBin := filepath.Join(healthDir, "grew")
	if err := os.WriteFile(healthBin, bin, 0755); err != nil {
		return fmt.Errorf("write health check binary: %w", err)
	}

	piCfg := sandbox.PostInstallConfig{
		KegDir: healthDir,
		TmpDir: healthDir,
	}
	healthCmd := sandbox.PostInstallCommand(piCfg, healthBin, "vuln-scan", "--offline")
	// Pass the current prefix explicitly so the health check doesn't fall back
	// to ~/.homegrew due to being run from a temporary path.
	healthCmd.Env = append(healthCmd.Env, "HOMEGREW_PREFIX="+config.DefaultPrefix())
	healthCmd.Env = append(healthCmd.Env, "HOMEGREW_NO_INIT_TAP=1")
	if out, err := healthCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("new binary health check failed: %v (output: %q)", err, string(out))
	}

	if err := release.AtomicInstall(exePath, bin); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	expectedVersion := strings.TrimPrefix(rel.TagName, "v")
	if err := VerifyBinaryIntegrity(exePath, expectedVersion); err != nil {
		slog.Warn(fmt.Sprintf("%v", err))
	}
	slog.Debug("binary integrity verified after installation")

	slog.Info(fmt.Sprintf("grew %s installed successfully", rel.TagName))
	auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rel.TagName, sha256Actual, "release")
	return nil
}

type OSVResult struct {
	Vulnerable bool
	Message    string
}
