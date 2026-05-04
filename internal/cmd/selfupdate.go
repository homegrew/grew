package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cache"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/installer"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/version"
	"github.com/homegrew/grew/pkg/ui"
)
func RunSelfUpdate(_ []string) error {
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
			slog.Error("patch update unavailable or failed", "err", patchErr)
		}
	}

	// 2. Alternatively, fall back to compile via git if repo is present (source installation).
	updated, err := installer.SelfUpdateFromGit(exePath)
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
	err = installer.InstallLatestRelease(exePath, &rels[0])
	if err != nil {
		auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rels[0].TagName, "", fmt.Sprintf("failed: %v", err))
	}
	return err
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
	return installer.InstallLatestRelease(exePath, &rels[0])
}


// installer.VerifyBinaryIntegrity re-execs the newly installed binary with --version
// and checks that it runs successfully and reports the expected version.
// If expectedVersion is empty, only checks that the binary executes.

// installer.FileHashes computes both hex-encoded SHA-256 and SHA-512 hashes of a file in a single pass.

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
	if res, err := installer.CheckOSVForVersion("github.com/homegrew/grew", targetVer); err != nil {
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

		actualPatchSHA256, actualPatchSHA512, err := installer.FileHashes(patchFile)
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

	actualBinSHA256, actualBinSHA512, err := installer.FileHashes(currentSource)
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
	healthCmd.Env = append(healthCmd.Env, "HOMEGREW_NO_INIT_TAP=1")
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
	if err := installer.VerifyBinaryIntegrity(exePath, expectedVersion); err != nil {
		slog.Warn(fmt.Sprintf("integrity verification failed: %v", err))
	}

	return nil
}


type OSVResult struct {
	Vulnerable bool
	Message    string
}
