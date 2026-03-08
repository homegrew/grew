package cmd

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/sandbox"
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
			if err := verifyBinaryIntegrity(destBin, ""); err != nil {
				fmt.Printf("==> Warning: %v\n", err)
			}
			auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", "", "", "source")
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

	expectedVersion := strings.TrimPrefix(rel.TagName, "v")
	if err := verifyBinaryIntegrity(exePath, expectedVersion); err != nil {
		fmt.Printf("==> Warning: %v\n", err)
	}

	auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rel.TagName, actualHash, "release")

	fmt.Printf("==> Updated to %s\n", rel.TagName)
	return nil
}

// verifyBinaryIntegrity re-execs the newly installed binary with --version
// and checks that it runs successfully and reports the expected version.
// If expectedVersion is empty, only checks that the binary executes.
func verifyBinaryIntegrity(binPath, expectedVersion string) error {
	Debugf("verifying binary integrity: %s (expect %q)\n", binPath, expectedVersion)

	// Hash the binary before execution so we can detect self-modification.
	hashBefore, err := fileSHA512(binPath)
	if err != nil {
		return fmt.Errorf("hash binary before execution: %w", err)
	}
	Debugf("SHA-512 before exec: %s\n", hashBefore)

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
	cmd := sandbox.PostInstallCommand(cfg, binPath, "--version")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("new binary failed to execute: %w", err)
	}

	// Verify the binary was not modified during execution.
	hashAfter, err := fileSHA512(binPath)
	if err != nil {
		return fmt.Errorf("hash binary after execution: %w", err)
	}
	if hashBefore != hashAfter {
		return fmt.Errorf("binary was modified during execution (self-modifying binary detected)\n"+
			"  before: %s\n  after:  %s", hashBefore, hashAfter)
	}
	Debugf("SHA-512 unchanged after exec\n")

	reportedVersion := strings.TrimSpace(string(out))
	Debugf("new binary reports: %s\n", reportedVersion)

	if expectedVersion == "" {
		// Git build: just verify it runs and reports something.
		if reportedVersion == "" {
			return fmt.Errorf("new binary produced no version output")
		}
		fmt.Printf("==> Verified: new binary reports %s\n", reportedVersion)
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

	fmt.Printf("==> Verified: new binary reports %s\n", reportedVersion)
	return nil
}

// fileSHA512 computes the hex-encoded SHA-512 hash of a file.
func fileSHA512(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
