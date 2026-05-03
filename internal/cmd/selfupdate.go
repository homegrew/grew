package cmd

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cache"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/osvdev"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/version"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
)

func RunSelfUpdate(_ []string) error {
	slog.Debug("starting selfupdate command execution")
	slog.Debug("starting selfupdate command execution")
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	rels, err := release.FetchRecent(10)
	if err != nil {
		slog.Warn(fmt.Sprintf("failed to fetch recent release metadata: %v", err))
	} else if len(rels) > 0 {
		rel := &rels[0]
		paths := config.Default()
		if err := paths.Init(); err != nil {
			slog.Warn(fmt.Sprintf("failed to initialize paths: %v", err))
		}

		dl := &downloader.Downloader{
			TmpDir: paths.Tmp,
			Cache:  cache.New(paths.Cache),
		}
		for i := range rels {
			rels[i].DL = dl
		}

		if !version.IsNewer(version.Version(), rel.TagName) {
			auditlog.New(paths.Log).Log(auditlog.ActionSelfUpdate, "grew", version.Version(), "", "already up-to-date")
			fmt.Printf("Already up-to-date: %s\n", version.Version())
			return nil
		}

		// 1. Always try to update by patch release first.
		if patchErr := tryPatchUpdate(exePath, rels); patchErr == nil {
			auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rel.TagName, "", "patch")
			return nil
		} else {
			slog.Debug("patch update unavailable or failed", "err", patchErr)
		}
	}

	//// 2. Alternatively, fall back to compile via git if repo is present.
	updated, err := SelfUpdateFromGit(exePath)
	if err != nil {
		slog.Warn(fmt.Sprintf("failed to update grew from git: %v", err))
	} else if updated {
		return nil
	}

	if len(rels) == 0 {
		return fmt.Errorf("cannot fallback to full download without release metadata")
	}

	// 3. Fall back to full release download
	fmt.Fprintln(os.Stderr, "==> Falling back to latest release full download...")
	err = selfUpdateFullDownload(exePath, &rels[0])
	if err != nil {
		auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rels[0].TagName, "", fmt.Sprintf("failed: %v", err))
	}
	return err
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

	if err := installFromGit(repoDir, destBin, false); err != nil {
		if errors.Is(err, ErrNoGitRepo) {
			slog.Debug(fmt.Sprintf("no git repo at %s, skipping source update", repoDir))
			return false, nil
		}
		return false, err
	}

	if err := verifyBinaryIntegrity(destBin, ""); err != nil {
		return true, fmt.Errorf("integrity check failed after source update: %w", err)
	}

	auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", "", "", "source")
	return true, nil
}

// SelfUpdateFromRelease downloads the latest stable release from GitHub,
// verifies its checksum, and replaces the running binary.
func SelfUpdateFromRelease(exePath string) error {
	rels, err := release.FetchRecent(10)
	if err != nil {
		return err
	}
	if len(rels) == 0 {
		return fmt.Errorf("no stable releases found")
	}

	// Try patch update first.
	if err := tryPatchUpdate(exePath, rels); err == nil {
		auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", "", "", "patch")
		return nil
	}

	slog.Info("binary patch update not available or failed; falling back to full download")
	return selfUpdateFullDownload(exePath, &rels[0])
}

func selfUpdateFullDownload(exePath string, rel *release.Release) error {
	// Apply OSV security gate before full download.
	targetVer := rel.TagName
	if res, err := checkOSVForVersion("github.com/homegrew/grew", targetVer); err != nil {
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
	if rel.DL == nil || rel.DL.Cache == nil || !strings.Contains(tmpFile, rel.DL.Cache.Dir()) {
		defer os.Remove(tmpFile)
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
	if out, err := healthCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("new binary health check failed: %v (output: %q)", err, string(out))
	}

	if err := release.AtomicInstall(exePath, bin); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	expectedVersion := strings.TrimPrefix(rel.TagName, "v")
	if err := verifyBinaryIntegrity(exePath, expectedVersion); err != nil {
		slog.Warn(fmt.Sprintf("%v", err))
	}

	auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rel.TagName, sha256Actual, "release")

	ui.FprintArrow(os.Stderr, "Updated to %s", rel.TagName)
	return nil
}

// verifyBinaryIntegrity re-execs the newly installed binary with --version
// and checks that it runs successfully and reports the expected version.
// If expectedVersion is empty, only checks that the binary executes.
func verifyBinaryIntegrity(binPath, expectedVersion string) error {
	slog.Debug(fmt.Sprintf("verifying binary integrity: %s (expect %q)", binPath, expectedVersion))

	// Hash the binary before execution so we can detect self-modification.
	// We check both SHA-256 and SHA-512.
	sha256Before, sha512Before, err := fileHashes(binPath)
	if err != nil {
		return fmt.Errorf("hash binary before execution: %w", err)
	}
	slog.Debug("SHA-256 before exec: " + sha256Before)
	slog.Debug("SHA-512 before exec: " + sha512Before)

	// Run the new binary inside a sandbox: no network, no writes (except a
	// throwaway temp dir). A compromised binary cannot exfiltrate data or
	// modify the system during this probe.
	tmpDir, err := os.MkdirTemp("", "grew-verify-*")
	if err != nil {
		return fmt.Errorf("create verify tmpdir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := sandbox.PostInstallConfig{
		KegDir: binPath, // read-only; only needs to read itself
		TmpDir: tmpDir,
	}

	var out []byte
	// Retry loop for ETXTBSY ("text file busy") which can happen on Linux
	// if a background indexer/scanner briefly opens the newly written file.
	for i := 0; i < 5; i++ {
		cmd := sandbox.PostInstallCommand(cfg, binPath, "--version")
		cmd.Env = append(cmd.Env, "HOMEGREW_PREFIX="+config.DefaultPrefix())
		out, err = cmd.Output()
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "text file busy") {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		break
	}

	if err != nil {
		return fmt.Errorf("new binary failed to execute: %w", err)
	}

	// Verify the binary was not modified during execution.
	sha256After, sha512After, err := fileHashes(binPath)
	if err != nil {
		return fmt.Errorf("hash binary after execution: %w", err)
	}
	if sha256Before != sha256After || sha512Before != sha512After {
		return fmt.Errorf("binary was modified during execution (self-modifying binary detected)\n"+
			"  SHA-256 before: %s\n  SHA-256 after:  %s\n"+
			"  SHA-512 before: %s\n  SHA-512 after:  %s",
			sha256Before, sha256After, sha512Before, sha512After)
	}
	slog.Debug("binary hashes unchanged after exec")

	reportedVersion := strings.TrimSpace(string(out))
	slog.Debug("new binary reports: " + reportedVersion)

	if expectedVersion == "" {
		// Git build: just verify it runs and reports something.
		if reportedVersion == "" {
			return fmt.Errorf("new binary produced no version output")
		}
		ui.FprintArrow(os.Stderr, "Verified: new binary reports %s", reportedVersion)
		return nil
	}

	// Release build: verify the version matches exactly.
	// Output format is "grew <version>" — extract the version part.
	parts := strings.Fields(reportedVersion)
	var actual string
	if len(parts) >= 2 {
		actual = strings.TrimPrefix(parts[1], "v")
	} else {
		actual = strings.TrimPrefix(reportedVersion, "v")
	}

	if actual != expectedVersion {
		return fmt.Errorf("version mismatch after update: expected %s, binary reports %q",
			expectedVersion, reportedVersion)
	}

	ui.FprintArrow(os.Stderr, "Verified: new binary reports %s", reportedVersion)
	return nil
}

// fileHashes computes both hex-encoded SHA-256 and SHA-512 hashes of a file in a single pass.
func fileHashes(path string) (sha256Hash, sha512Hash string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	h256 := sha256.New()
	h512 := sha512.New()
	mw := io.MultiWriter(h256, h512)

	if _, err := io.Copy(mw, f); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(h256.Sum(nil)), hex.EncodeToString(h512.Sum(nil)), nil
}

// fileSHA256 computes the hex-encoded SHA-256 hash of a file.
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

type patchStep struct {
	url       string
	name      string
	toVersion string
	release   *release.Release
}

func findPatchPath(currentVer, targetVer string, releases []release.Release) []patchStep {
	// patches[toVersion] = list of patches that lead to toVersion
	patches := make(map[string][]patchStep)
	for i := range releases {
		rel := &releases[i]
		for _, asset := range rel.Assets {
			old := release.ParsePatchVersion(asset.Name)
			if old != "" {
				patches[rel.TagName] = append(patches[rel.TagName], patchStep{
					url:       asset.BrowserDownloadURL,
					name:      asset.Name,
					toVersion: rel.TagName,
					release:   rel,
				})
			}
		}
	}

	type edge struct {
		ver  string
		path []patchStep
	}
	queue := []edge{{ver: targetVer, path: nil}}
	visited := map[string]bool{targetVer: true}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if curr.ver == currentVer {
			return curr.path
		}

		for _, p := range patches[curr.ver] {
			fromVer := release.ParsePatchVersion(p.name)
			if !visited[fromVer] {
				visited[fromVer] = true
				// Prepend this patch because we are going backwards from target to current
				newPath := append([]patchStep{p}, curr.path...)
				queue = append(queue, edge{ver: fromVer, path: newPath})
			}
		}
	}
	return nil
}

func tryPatchUpdate(exePath string, releases []release.Release) error {
	bspatch, err := exec.LookPath("bspatch")
	if err != nil {
		return fmt.Errorf("bspatch not found in PATH")
	}

	currentVer := version.Version()
	latestRel := &releases[0]
	targetVer := latestRel.TagName

	if currentVer == targetVer {
		return fmt.Errorf("already at latest version %s", targetVer)
	}

	path := findPatchPath(currentVer, targetVer, releases)
	if len(path) == 0 {
		return fmt.Errorf("no patch path found from %s to %s", currentVer, targetVer)
	}

	// 1. Query OSV for target version
	if res, err := checkOSVForVersion("github.com/homegrew/grew", targetVer); err != nil {
		slog.Warn(fmt.Sprintf("OSV query failed: %v", err))
	} else if res.Vulnerable {
		return fmt.Errorf("target version %s is vulnerable: %s", targetVer, res.Message)
	}

	ui.FprintArrow(os.Stderr, "Found upgrade path via %d patches", len(path))

	currentSource := exePath
	var intermediateFiles []string
	defer func() {
		for _, f := range intermediateFiles {
			os.Remove(f)
		}
	}()

	for i, step := range path {
		ui.FprintArrow(os.Stderr, "Applying patch %d/%d: %s", i+1, len(path), step.name)

		patchFile, err := step.release.DownloadTemp(step.url, step.name)
		if err != nil {
			return fmt.Errorf("download patch %s: %w", step.name, err)
		}
		if step.release.DL == nil || step.release.DL.Cache == nil || !strings.Contains(patchFile, step.release.DL.Cache.Dir()) {
			defer os.Remove(patchFile)
		}

		actualPatchSHA256, actualPatchSHA512, err := fileHashes(patchFile)
		if err != nil {
			return err
		}

		// Verify patch checksum
		sha256URL, err256 := release.FindAssetURL(step.release, step.name+".sha256")
		sha512URL, err512 := release.FindAssetURL(step.release, step.name+".sha512")

		if err256 == nil || err512 == nil {
			if err256 == nil {
				if data, err := step.release.DownloadBytes(sha256URL); err == nil {
					expected := strings.Fields(string(data))[0]
					if actualPatchSHA256 != expected {
						return fmt.Errorf("patch %s SHA-256 mismatch: got %s, want %s", step.name, actualPatchSHA256, expected)
					}
				}
			}
			if err512 == nil {
				if data, err := step.release.DownloadBytes(sha512URL); err == nil {
					expected := strings.Fields(string(data))[0]
					if actualPatchSHA512 != expected {
						return fmt.Errorf("patch %s SHA-512 mismatch: got %s, want %s", step.name, actualPatchSHA512, expected)
					}
				}
			}
		} else {
			// Fallback: Verify patch hash against the monolithic checksums.txt of the release containing the patch
			checksumURL, err := release.FindAssetURL(step.release, "checksums.txt")
			if err != nil {
				return err
			}
			checksums, err := step.release.DownloadBytes(checksumURL)
			if err != nil {
				return err
			}
			expectedPatchHashes := release.FindAllChecksums(checksums, step.name)
			if len(expectedPatchHashes) == 0 {
				return fmt.Errorf("no checksum found for %s in checksums.txt", step.name)
			}

			if expected, ok := expectedPatchHashes[64]; ok && actualPatchSHA256 != expected {
				return fmt.Errorf("patch %s SHA-256 mismatch: got %s, want %s", step.name, actualPatchSHA256, expected)
			}
			if expected, ok := expectedPatchHashes[128]; ok && actualPatchSHA512 != expected {
				return fmt.Errorf("patch %s SHA-512 mismatch: got %s, want %s", step.name, actualPatchSHA512, expected)
			}
		}

		// Create temp file for the result of this patch step
		f, err := os.CreateTemp("", "grew-patched-*")
		if err != nil {
			return fmt.Errorf("create temp binary: %w", err)
		}
		tmpNewBin := f.Name()
		f.Close()
		intermediateFiles = append(intermediateFiles, tmpNewBin)

		cmd := exec.Command(bspatch, currentSource, tmpNewBin, patchFile)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("apply patch %s: %v (output: %q)", step.name, err, string(out))
		}

		if err := os.Chmod(tmpNewBin, 0755); err != nil {
			return fmt.Errorf("chmod patched binary: %w", err)
		}

		currentSource = tmpNewBin
	}

	// 3. Verify final patched binary hash against binary-checksums.txt of the LATEST release
	binaryChecksumURL, err := release.FindAssetURL(latestRel, "binary-checksums.txt")
	if err != nil {
		return fmt.Errorf("binary-checksums.txt not found in latest release")
	}
	binaryChecksums, err := latestRel.DownloadBytes(binaryChecksumURL)
	if err != nil {
		return err
	}
	rawBinName := release.RawBinaryName()
	expectedBinHashes := release.FindAllChecksums(binaryChecksums, rawBinName)
	if len(expectedBinHashes) == 0 {
		return fmt.Errorf("no checksum found for %s in binary-checksums.txt", rawBinName)
	}

	actualBinSHA256, actualBinSHA512, err := fileHashes(currentSource)
	if err != nil {
		return err
	}
	if expected, ok := expectedBinHashes[64]; ok && actualBinSHA256 != expected {
		return fmt.Errorf("final binary SHA-256 mismatch: got %s, want %s", actualBinSHA256, expected)
	}
	if expected, ok := expectedBinHashes[128]; ok && actualBinSHA512 != expected {
		return fmt.Errorf("final binary SHA-512 mismatch: got %s, want %s", actualBinSHA512, expected)
	}

	// 4. Health Check: run vuln-scan on final binary (sandboxed)
	piTmp, err := os.MkdirTemp("", "grew-selfupdate-health-*")
	if err != nil {
		return fmt.Errorf("create health check tmpdir: %w", err)
	}
	defer os.RemoveAll(piTmp)

	piCfg := sandbox.PostInstallConfig{
		KegDir: filepath.Dir(currentSource),
		TmpDir: piTmp,
	}
	healthCmd := sandbox.PostInstallCommand(piCfg, currentSource, "vuln-scan", "--offline")
	healthCmd.Env = append(healthCmd.Env, "HOMEGREW_PREFIX="+config.DefaultPrefix())
	if out, err := healthCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("final patched binary health check failed: %v (output: %q)", err, string(out))
	}

	// 5. Atomic replace
	data, err := os.ReadFile(currentSource)
	if err != nil {
		return err
	}
	if err := release.AtomicInstall(exePath, data); err != nil {
		return err
	}

	ui.FprintArrow(os.Stderr, "Updated to %s via %d binary patches", targetVer, len(path))

	expectedVersion := strings.TrimPrefix(targetVer, "v")
	if err := verifyBinaryIntegrity(exePath, expectedVersion); err != nil {
		slog.Warn(fmt.Sprintf("integrity verification failed: %v", err))
	}

	return nil
}

func checkOSVForVersion(pkgName, ver string) (*OSVResult, error) {
	client := osvdev.NewClient()
	vulns, err := client.Query(osvdev.QueryPackage{
		RepoURL: pkgName,
		Version: ver,
	})
	if err != nil {
		return nil, fmt.Errorf("query OSV: %w", err)
	}
	if len(vulns) > 0 {
		var ids []string
		for _, v := range vulns {
			ids = append(ids, v.ID)
		}
		return &OSVResult{
			Vulnerable: true,
			Message:    fmt.Sprintf("found %d vulnerabilities: %s", len(vulns), strings.Join(ids, ", ")),
		}, nil
	}
	return &OSVResult{Vulnerable: false}, nil
}

type OSVResult struct {
	Vulnerable bool
	Message    string
}
