package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/pkg/auditlog"
	"github.com/homegrew/grew/pkg/bpatch"
	"github.com/homegrew/grew/pkg/cache"
	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/downloader"
	"github.com/homegrew/grew/pkg/installer"
	"github.com/homegrew/grew/pkg/release"
	"github.com/homegrew/grew/pkg/version"
	"github.com/homegrew/grew/pkg/safepath"
)

func runSelfUpdate(_ []string) error {
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
		if patchErr := bpatch.TryPatchUpdate(exePath, rels); patchErr == nil {
			auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", rel.TagName, "", "patch")
			return nil
		} else {
			slog.Error("patch update unavailable or failed", "err", patchErr)
		}
	}

	// 2. Alternatively, fall back to compile via git if repo is present (source installation).
	updated, err := selfUpdateFromGit(exePath)
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

func selfUpdateFromRelease(exePath string) error {
	rels, err := release.FetchRecent(10)
	if err != nil {
		return err
	}
	if len(rels) == 0 {
		return fmt.Errorf("no stable releases found")
	}

	// Try patch update first.
	if err := bpatch.TryPatchUpdate(exePath, rels); err == nil {
		auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", "", "", "patch")
		return nil
	}

	slog.Info("binary patch update not available or failed; falling back to full download")
	return installer.InstallLatestRelease(exePath, &rels[0])
}

func selfUpdateFromGit(exePath string) (bool, error) {
	prefix := config.DefaultPrefix()
	repoDir := filepath.Join(prefix, "Grew")
	destBin := filepath.Join(prefix, "bin", "grew")

	var err error
	destBin, err = safepath.EnsurePathWithinBase(prefix, destBin)
	if err != nil {
		return false, fmt.Errorf("invalid destination binary path: %w", err)
	}

	if err := installer.InstallFromGit(grewRepoURL, repoDir, destBin, false); err != nil {
		if errors.Is(err, installer.ErrNoGitRepo) {
			slog.Debug(fmt.Sprintf("no git repo at %s, skipping source update", repoDir))
			return false, nil
		}
		return false, err
	}

	if err := installer.VerifyBinaryIntegrity(destBin, ""); err != nil {
		return true, fmt.Errorf("integrity check failed after source update: %w", err)
	}

	auditlog.New(config.Default().Log).Log(auditlog.ActionSelfUpdate, "grew", "", "", "source")
	return true, nil
}

var grewRepoURL = "https://github.com/homegrew/grew.git"

func init() {
	if url := os.Getenv("HOMEGREW_REPO_URL"); url != "" {
		grewRepoURL = url
	}
}

