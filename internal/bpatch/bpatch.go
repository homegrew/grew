package bpatch

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/installer"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/version"
	"github.com/homegrew/grew/pkg/ui"
)

type patchStep struct {
	url       string
	name      string
	toVersion string
	release   *release.Release
}

func isOfficialBuild(v string) bool {
	if _, err := release.FetchRelease(v); err == nil {
		return true
	}

	return false
}

// TryPatchUpdate attempts to update the binary at exePath using binary patches.
func TryPatchUpdate(exePath string, releases []release.Release) error {
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

		if err := VerifyPatchChecksum(step, patchFile); err != nil {
			return err
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
		binaryChecksumURL, err = release.FindAssetURL(latestRel, "binary-checksum.txt")
		if err != nil {
			return fmt.Errorf("binary-checksums.txt (or binary-checksum.txt) not found in latest release")
		}
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

// VerifyPatchChecksum ensures that a downloaded patch file matches the expected hashes
// (SHA-256 and/or SHA-512) published in the release assets. It checks for standalone
// .sha256 and .sha512 files first, and falls back to verifying against the release's
// monolithic checksum.txt (or checksums.txt) file if standalone files are not present.
func VerifyPatchChecksum(step patchStep, patchFile string) error {
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
			checksumURL, err = release.FindAssetURL(step.release, "checksum.txt")
			if err != nil {
				return err
			}
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
	return nil
}

// TestPatchUpgrade verifies that there is a valid patch path from version1 to version2,
// downloads all the intermediate patches, and verifies their checksums.
func TestPatchUpgrade(version1, version2 string, releases []release.Release) (patchStep, error) {
	relsDup := make([]release.Release, len(releases))
	copy(relsDup, releases)
	pth := findPatchPath(version1, version2, relsDup)
	if len(pth) == 0 {
		return patchStep{}, fmt.Errorf("no patch path found from %s to %s", version1, version2)
	}

	seq := func(yield func(string) bool) {
		for i := 0; i < len(pth); i++ {
			if !yield(pth[i].release.TagName) {
				return
			}
		}
	}
	steps := slices.Collect(seq)
	slog.Debug(fmt.Sprintf("Patching %s to %s, path: %s", version1, version2, strings.Join(steps, " -- ")))

	ui.FprintArrow(os.Stderr, "Found upgrade path via %d patches from %s to %s", len(pth), version1, version2)

	for i, step := range pth {
		ui.FprintArrow(os.Stderr, "Testing patch %d/%d: %s", i+1, len(pth), step.name)
		slog.Debug(fmt.Sprintf("Testing patch %d/%d: %s", i+1, len(pth), step.name))

		patchFile, err := step.release.DownloadTemp(step.url, step.name)
		if err != nil {
			return patchStep{}, fmt.Errorf("download patch [%d] %s: %w", i, step.name, err)
		}

		slog.Debug(fmt.Sprintf("Verifying patch %d/%d(%s): %q", i+1, len(pth), step.name, patchFile))
		if err := VerifyPatchChecksum(step, patchFile); err != nil {
			return patchStep{}, err
		}

		if step.release.DL == nil || step.release.DL.Cache == nil || !strings.Contains(patchFile, step.release.DL.Cache.Dir()) {
			os.Remove(patchFile)
		}
	}

	ui.FprintArrow(os.Stderr, "All patches from %s to %s are downloadable and valid", version1, version2)
	return patchStep{}, nil
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