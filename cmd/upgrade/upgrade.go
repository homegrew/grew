package upgrade

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/pkg/cmd"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/flags"
	"github.com/homegrew/grew/pkg/installer"

	"github.com/homegrew/grew/pkg/auditlog"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "upgrade [formula ...]",
	Short: "Upgrade outdated formulas",
	Long: `Upgrade outdated formulas to the latest version available in the tap.
With no arguments, upgrades all outdated packages. Specify formula
names to upgrade only those.

The old version keg is removed after a successful upgrade.`,
	Example: `  grew upgrade
  grew upgrade jq`,
	RunE: func(c *cobra.Command, args []string) error {
		installCtx, installCtxErr := context.NewInstallContext()
		if installCtxErr != nil {
			return installCtxErr
		}

		upgradeErr := runUpgrade(installCtx, args)
		installCtx.Close()
		if upgradeErr != nil {
			return upgradeErr
		}

		if !flags.Quiet {
			fmt.Println("==> Running cleanup...")
		}

		// Temporarily suppress output for the automatic cleanup phase
		// to make it behave more like a background task, unless in verbose mode.
		readCtx, ctxErr := context.New()
		if ctxErr != nil {
			slog.Warn("couldn't create context", "error", ctxErr)
		} else if err := cmd.RunCleanup(readCtx, args, cmd.CleanupOpts{}); err != nil {
			slog.Warn("automatic cleanup failed", "error", err)
		}

		return nil
	},
}

func runUpgrade(ctx *context.InstallContext, args []string) error {
	slog.Debug("starting upgrade command execution")

	var targets []outdatedPkg

	if len(args) > 0 {
		// Upgrade specific formulas
		for _, name := range args {
			if !ctx.Cellar.IsInstalled(name) {
				return fmt.Errorf("formula %q is not installed", name)
			}
			if ctx.Cellar.IsPinned(name) {
				if ctx.AuditLog != nil {
					ctx.AuditLog.Log(auditlog.ActionUpgrade, name, "", "", "pinned, skipping")
				}
				ui.FprintArrow(os.Stderr, "%s is pinned, skipping (use 'grew unpin %s' first)", name, name)
				continue
			}
			f, err := ctx.Loader.LoadByName(name)
			if err != nil {
				return fmt.Errorf("formula not found: %s", name)
			}
			curVer, _ := ctx.Cellar.InstalledVersion(name)
			if curVer == f.Version {
				if ctx.AuditLog != nil {
					ctx.AuditLog.Log(auditlog.ActionUpgrade, name, curVer, "", "already up-to-date")
				}
				ui.FprintArrow(os.Stderr, "%s %s already up-to-date", name, curVer)
				continue
			}
			targets = append(targets, outdatedPkg{formula: f, installedVersion: curVer})
		}
	} else {
		// Upgrade all outdated packages
		installed, err := ctx.Cellar.List()
		if err != nil {
			return err
		}
		if len(installed) == 0 {
			fmt.Println("No packages installed.")
			return nil
		}
		for _, pkg := range installed {
			if ctx.Cellar.IsPinned(pkg.Name) {
				if ctx.AuditLog != nil {
					ctx.AuditLog.Log(auditlog.ActionUpgrade, pkg.Name, pkg.Version, "", "pinned, skipping")
				}
				slog.Debug("skipping " + pkg.Name + ": pinned")
				continue
			}
			f, err := ctx.Loader.LoadByName(pkg.Name)
			if err != nil {
				slog.Debug(fmt.Sprintf("skipping %s: no longer in any tap (%v)", pkg.Name, err))
				continue
			}
			if pkg.Version != f.Version {
				targets = append(targets, outdatedPkg{formula: f, installedVersion: pkg.Version})
			}
		}
	}

	if len(targets) == 0 {
		fmt.Println("All packages are up-to-date.")
		return nil
	}

	for _, t := range targets {
		ui.FprintArrow(os.Stderr, "Upgrading %s %s -> %s", t.formula.Name, t.installedVersion, t.formula.Version)

		// Unlink old version
		if err := ctx.Linker.Unlink(t.formula.Name); err != nil {
			slog.Warn("couldn't unlink formula", "formula", t.formula.Name, "error", err)
		} else {
			slog.Info("unlinked old version " + t.installedVersion)
		}

		// Install new version (old keg stays until we confirm success)
		if err := installer.InstallFormula(t.formula, ctx, installer.InstallOpts{InstalledOnRequest: true}); err != nil {
			if ctx.AuditLog != nil {
				ctx.AuditLog.Log(auditlog.ActionUpgrade, t.formula.Name, t.formula.Version, "", fmt.Sprintf("failed: %v", err))
			}
			return err
		}

		if ctx.AuditLog != nil {
			ctx.AuditLog.Log(auditlog.ActionUpgrade, t.formula.Name, t.formula.Version, "",
				fmt.Sprintf("%s -> %s", t.installedVersion, t.formula.Version))
		}

		if t.formula.Caveats != "" {
			ui.FprintArrow(os.Stderr, "Caveats")
			fmt.Fprintln(os.Stderr, t.formula.Caveats)
		}
	}

	return nil
}

type outdatedPkg struct {
	formula          *formula.Formula
	installedVersion string
}
