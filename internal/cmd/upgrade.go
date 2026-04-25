package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/formula"
)

type outdatedPkg struct {
	formula          *formula.Formula
	installedVersion string
}

func runUpgrade(args []string) error {
	ctx, err := newInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	var targets []outdatedPkg

	if len(args) > 0 {
		// Upgrade specific formulas
		for _, name := range args {
			if !ctx.Cellar.IsInstalled(name) {
				return fmt.Errorf("formula %q is not installed", name)
			}
			if ctx.Cellar.IsPinned(name) {
				fmt.Printf("==> %s is pinned, skipping (use 'grew unpin %s' first)\n", name, name)
				continue
			}
			f, err := ctx.Loader.LoadByName(name)
			if err != nil {
				return fmt.Errorf("formula not found: %s", name)
			}
			curVer, _ := ctx.Cellar.InstalledVersion(name)
			if curVer == f.Version {
				fmt.Printf("==> %s %s already up-to-date\n", name, curVer)
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
		fmt.Printf("==> Upgrading %s %s -> %s\n", t.formula.Name, t.installedVersion, t.formula.Version)

		// Unlink old version
		ctx.Linker.Unlink(t.formula.Name)
		slog.Info("unlinked old version " + t.installedVersion)

		// Install new version (old keg stays until we confirm success)
		if err := installFormula(t.formula, ctx, installOpts{installedOnRequest: true}); err != nil {
			return err
		}

		if ctx.AuditLog != nil {
			ctx.AuditLog.Log(auditlog.ActionUpgrade, t.formula.Name, t.formula.Version, "",
				fmt.Sprintf("%s -> %s", t.installedVersion, t.formula.Version))
		}

		// Remove old version keg if different from new
		oldKeg, _ := ctx.Cellar.KegPath(t.formula.Name, t.installedVersion)
		if t.installedVersion != t.formula.Version {
			if err := removeDir(oldKeg); err != nil {
				slog.Warn(fmt.Sprintf("could not remove old keg %s: %v", oldKeg, err))
			} else {
				slog.Info("removed old keg: " + oldKeg)
			}
		}
	}

	return nil
}

func runOutdated(args []string) error {
	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	installed, err := ctx.Cellar.List()
	if err != nil {
		return err
	}
	if len(installed) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}

	found := false
	for _, pkg := range installed {
		f, err := ctx.Loader.LoadByName(pkg.Name)
		if err != nil {
			slog.Debug(fmt.Sprintf("skipping %s: not in any tap (%v)", pkg.Name, err))
			continue
		}
		if pkg.Version != f.Version {
			pinMarker := ""
			if ctx.Cellar.IsPinned(pkg.Name) {
				pinMarker = " [pinned]"
			}
			fmt.Printf("%-20s %s -> %s%s\n", pkg.Name, pkg.Version, f.Version, pinMarker)
			found = true
		}
	}

	if !found {
		fmt.Println("All packages are up-to-date.")
	}
	return nil
}

func removeDir(path string) error {
	return os.RemoveAll(path)
}
