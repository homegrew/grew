package installer

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/sandbox"
	verpkg "github.com/homegrew/grew/internal/version"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
	"runtime"
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

	gitDir := filepath.Clean(filepath.Join(cleanRepoDir, ".git"))
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

// verifyBinaryIntegrity runs a basic check on the binary.
func VerifyBinaryIntegrity(binPath string, expectedVersion string) error {
	cmd := exec.Command(binPath, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute binary check: %w\noutput: %s", err, string(out))
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return fmt.Errorf("no version output")
	}
	if expectedVersion != "" && !strings.Contains(outStr, expectedVersion) && !strings.Contains(outStr, "dev") {
		return fmt.Errorf("version mismatch: expected %s, got %s", expectedVersion, outStr)
	}

	slog.Debug(fmt.Sprintf("binary execution check passed: %s", outStr))
	return nil
}

func fileHashes(path string) (string, string, error) {
	sha256Hash, err := release.FileSHA256(path)
	if err != nil {
		return "", "", err
	}
	sha512Hash, err := release.FileSHA512(path)
	if err != nil {
		return "", "", err
	}
	return sha256Hash, sha512Hash, nil
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
	if rel.DL == nil || rel.DL.Cache == nil || !strings.Contains(tmpFile, rel.DL.Cache.Dir()) {
		cleanedTmpFile := filepath.Clean(tmpFile)
		if eval, err := filepath.EvalSymlinks(cleanedTmpFile); err == nil {
			cleanedTmpFile = filepath.Clean(eval)
		}

		// Determine the expected temporary directory for this download.
		// If rel.DL is nil or its TmpDir is empty, it falls back to os.TempDir().
		expectedTmpDir := os.TempDir()
		if rel.DL != nil && rel.DL.TmpDir != "" {
			expectedTmpDir = rel.DL.TmpDir
		}

		// Canonicalize the expected temporary directory path.
		if eval, err := filepath.EvalSymlinks(expectedTmpDir); err == nil {
			expectedTmpDir = filepath.Clean(eval)
		} else {
			expectedTmpDir = filepath.Clean(expectedTmpDir)
		}

		// Ensure the file to be removed is strictly within the expected temporary directory.
		if err := safepath.CheckSubpath(expectedTmpDir, cleanedTmpFile); err != nil {
			slog.Error(fmt.Sprintf("security: refusing to remove temporary file %q outside expected temp directory %q: %v", cleanedTmpFile, expectedTmpDir, err))
			return fmt.Errorf("temporary file %q escaped expected temporary directory: %w", cleanedTmpFile, err)
		} else {
			defer os.Remove(cleanedTmpFile)
		}
	}

	// Verify all available hashes.
	sha256Actual, sha512Actual, err := fileHashes(tmpFile)
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
	defer os.RemoveAll(healthDir)
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

	auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rel.TagName, sha256Actual, "release")
	return nil
}

func ensurePathWithinBase(base, target string) (string, error) {
	baseAbs, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", fmt.Errorf("resolve base path: %w", err)
	}
	if err := safepath.SafeAbsolutePath(baseAbs); err != nil {
		return "", fmt.Errorf("invalid base path %q: %w", baseAbs, err)
	}

	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	if err := safepath.SafeAbsolutePath(targetAbs); err != nil {
		return "", fmt.Errorf("invalid target path %q: %w", targetAbs, err)
	}

	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", fmt.Errorf("compute relative path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q escapes base directory %q", targetAbs, baseAbs)
	}
	return targetAbs, nil
}

func SelfUpdateFromGit(exePath string) (bool, error) {
	prefix := config.DefaultPrefix()
	repoDir := filepath.Join(prefix, "Grew")
	destBin := filepath.Join(prefix, "bin", "grew")

	var err error
	destBin, err = ensurePathWithinBase(prefix, destBin)
	if err != nil {
		return false, fmt.Errorf("invalid destination binary path: %w", err)
	}

	if err := InstallFromGit(grewRepoURL, repoDir, destBin, false); err != nil {
		if errors.Is(err, ErrNoGitRepo) {
			slog.Debug(fmt.Sprintf("no git repo at %s, skipping source update", repoDir))
			return false, nil
		}
		return false, err
	}

	if err := VerifyBinaryIntegrity(destBin, ""); err != nil {
		return true, fmt.Errorf("integrity check failed after source update: %w", err)
	}

	auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", "", "", "source")
	return true, nil
}
const grewRepoURL = "https://github.com/homegrew/grew.git"
type OSVResult struct {
	Vulnerable bool
	Message    string
}
