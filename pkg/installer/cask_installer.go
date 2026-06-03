package installer

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/pkg/auditlog"
	"github.com/homegrew/grew/pkg/cask"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/downloader"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/logger"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
)

func CaskInstall(ctx *context.InstallContext, name string, noQuarantine bool, force bool, skipLink bool) (err error) {
	c, err := ctx.LoadCask(name)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			slog.Error("cask installation failed, cleaning up", "cask", c.Name, "error", err)
			_ = CaskUninstall(ctx.Context, c.Name, true)
		}
	}()

	if ctx.Caskroom.IsInstalled(c.Name) && !force {
		ui.FprintArrow(os.Stderr, "%s %s is already installed, skipping", c.Name, c.Version)
		return nil
	}

	if err := safepath.SafePathComponent(c.Name); err != nil {
		return fmt.Errorf("invalid cask name: %w", err)
	}
	if err := safepath.SafePathComponent(c.Version); err != nil {
		return fmt.Errorf("invalid cask version: %w", err)
	}

	defer logger.TimeOp(fmt.Sprintf("install cask %s %s", c.Name, c.Version))()
	slog.Debug("platform: " + formula.PlatformKey())
	ui.FprintArrow(os.Stderr, "Installing cask %s %s", c.Name, c.Version)

	dlURL, err := c.GetURL()
	if err != nil {
		return err
	}
	slog.Info("URL: " + dlURL)

	sha, err := c.GetSHA256()
	if err != nil {
		return err
	}
	slog.Info("expected SHA256: " + sha)

	sha512 := c.GetSHA512()
	if sha512 != "" {
		slog.Info("expected SHA512: " + sha512)
	}

	filename := c.Name + "-" + c.Version + safepath.URLExt(dlURL)
	if err := safepath.SafePathComponent(filename); err != nil {
		return fmt.Errorf("invalid download filename: %w", err)
	}
	localFile, err := ctx.DL.Download(dlURL, filename)
	if err != nil {
		return fmt.Errorf("download %s: %w", c.Name, err)
	}

	if err := downloader.VerifySHA256(localFile, sha); err != nil {
		_ = fsutil.RemoveIfWithinAllowed(ctx.Paths.Tmp, ctx.Paths.Cache, localFile)
		return fmt.Errorf("verify %s: %w", c.Name, err)
	}

	ui.FprintArrow(os.Stderr, "SHA256 verified")

	if sha512 != "" {
		if err := downloader.VerifySHA512(localFile, sha512); err != nil {
			_ = fsutil.RemoveIfWithinAllowed(ctx.Paths.Tmp, ctx.Paths.Cache, localFile)
			return fmt.Errorf("verify %s (SHA512): %w", c.Name, err)
		}
		ui.FprintArrow(os.Stderr, "SHA512 verified")
	}

	stageDir, err := safepath.SafeJoin(ctx.Paths.Tmp, c.Name+"-"+c.Version+"-stage")
	if err != nil {
		return fmt.Errorf("invalid stage directory: %w", err)
	}
	os.RemoveAll(stageDir)

	ui.FprintArrow(os.Stderr, "Extracting (sandboxed)")
	if err := SandboxedExtract(localFile, stageDir, formula.InstallSpec{Type: "archive"}); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("extract %s: %w", c.Name, err)
	}
	slog.Info("extracted to staging: " + stageDir)

	inst := &cask.Installer{AppDir: ctx.Paths.AppDir, BinDir: ctx.Paths.Bin}

	for _, appName := range c.Artifacts.App {
		dest, err := inst.InstallApp(stageDir, appName)
		if err != nil {
			os.RemoveAll(stageDir)
			_ = fsutil.RemoveIfWithinAllowed(ctx.Paths.Tmp, ctx.Paths.Cache, localFile)
			return fmt.Errorf("install artifact %s: %w", appName, err)
		}
		if noQuarantine {
			slog.Info("quarantine skipped (--no-quarantine)")
		} else {
			if err := ApplyCaskQuarantine(dest, dlURL); err != nil {
				os.RemoveAll(dest)
				os.RemoveAll(stageDir)
				_ = fsutil.RemoveIfWithinAllowed(ctx.Paths.Tmp, ctx.Paths.Cache, localFile)
				return err
			}
			slog.Info("quarantine attribute set")
			ctx.AuditLog.Log(auditlog.ActionQuarantine, c.Name, c.Version, "", "quarantined via LaunchServices")
		}
		ui.FprintArrow(os.Stderr, "Installed %s to %s", appName, dest)
	}

	var installedPkgs []string
	for _, pkgName := range c.Artifacts.Pkg {
		if err := inst.InstallPkg(stageDir, pkgName); err != nil {
			for _, p := range installedPkgs {
				_ = inst.UninstallPkg(p)
			}
			os.RemoveAll(stageDir)
			_ = fsutil.RemoveIfWithinAllowed(ctx.Paths.Tmp, ctx.Paths.Cache, localFile)
			return fmt.Errorf("install artifact %s: %w", pkgName, err)
		}
		installedPkgs = append(installedPkgs, pkgName)
	}

	if !skipLink {
		for _, binName := range c.Artifacts.Bin {
			binTarget := findCaskBinary(ctx.Paths.AppDir, c.Artifacts.App, binName)
			if binTarget != "" {
				if err := inst.LinkBin(binName, binTarget); err != nil {
					slog.Warn(fmt.Sprintf("could not link binary %s: %v", binName, err))
				} else {
					slog.Info(fmt.Sprintf("linked binary: %s -> %s", binName, binTarget))
				}
			}
		}
	} else {
		// Ensure any existing links are removed.
		for _, binName := range c.Artifacts.Bin {
			_ = inst.UnlinkBin(binName)
		}
	}

	if err := ctx.Caskroom.Record(c.Name, c.Version); err != nil {
		return fmt.Errorf("record cask installation: %w", err)
	}

	os.RemoveAll(stageDir)
	_ = fsutil.RemoveIfWithinAllowed(ctx.Paths.Tmp, ctx.Paths.Cache, localFile)

	ui.FprintArrow(os.Stderr, "%s %s installed", c.Name, c.Version)

	if c.Caveats != "" {
		ui.FprintArrow(os.Stderr, "Caveats")
		fmt.Fprintln(os.Stderr, c.Caveats)
	}

	return nil
}

func CaskUninstall(ctx *context.Context, name string, force bool) error {
	if !ctx.Caskroom.IsInstalled(name) {
		if !force {
			return fmt.Errorf("cask %q is not installed", name)
		}
		return nil
	}

	c, err := ctx.LoadCask(name)
	inst := &cask.Installer{AppDir: ctx.Paths.AppDir, BinDir: ctx.Paths.Bin}

	if err == nil {
		for _, appName := range c.Artifacts.App {
			ui.FprintArrow(os.Stderr, "Removing %s...", appName)
			if err := inst.UninstallApp(appName); err != nil {
				if force {
					slog.Warn(fmt.Sprintf("ignoring error while removing %s: %v", appName, err))
				} else {
					slog.Warn(fmt.Sprintf("could not remove %s: %v", appName, err))
				}
			}
		}
		for _, pkgName := range c.Artifacts.Pkg {
			if err := inst.UninstallPkg(pkgName); err != nil {
				if force {
					slog.Warn(fmt.Sprintf("ignoring error while uninstallation of %s: %v", pkgName, err))
				} else {
					return fmt.Errorf("could not uninstall .pkg artifact %s: %w", pkgName, err)
				}
			}
		}
		for _, binName := range c.Artifacts.Bin {
			if err := inst.UnlinkBin(binName); err != nil {
				if force {
					slog.Warn(fmt.Sprintf("ignoring error while unlinking binary %s: %v", binName, err))
				} else {
					slog.Warn(fmt.Sprintf("could not unlink binary %s: %v", binName, err))
				}
			}
		}
	}

	if err := ctx.Caskroom.Remove(name); err != nil {
		if force {
			slog.Warn(fmt.Sprintf("ignoring error while removing cask %s from Caskroom: %v", name, err))
		} else {
			return err
		}
	}

	ui.FprintArrow(os.Stderr, "%s uninstalled", name)
	return nil
}

func findCaskBinary(appDir string, apps []string, binName string) string {
	for _, app := range apps {
		candidate := filepath.Join(appDir, app, "Contents", "MacOS", binName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
