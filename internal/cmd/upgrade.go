package cmd

import (
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
)

type outdatedPkg struct {
	formula          *formula.Formula
	installedVersion string
}

func runUpgrade(args []string) error {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}
	dl := &downloader.Downloader{TmpDir: paths.Tmp}

	var targets []outdatedPkg

	if len(args) > 0 {
		// Upgrade specific formulas
		for _, name := range args {
			if !cel.IsInstalled(name) {
				return fmt.Errorf("formula %q is not installed", name)
			}
			f, err := loader.LoadByName(name)
			if err != nil {
				return fmt.Errorf("formula not found: %s", name)
			}
			curVer, _ := cel.InstalledVersion(name)
			if curVer == f.Version {
				fmt.Printf("==> %s %s already up-to-date\n", name, curVer)
				continue
			}
			targets = append(targets, outdatedPkg{formula: f, installedVersion: curVer})
		}
	} else {
		// Upgrade all outdated packages
		installed, err := cel.List()
		if err != nil {
			return err
		}
		if len(installed) == 0 {
			fmt.Println("No packages installed.")
			return nil
		}
		for _, pkg := range installed {
			f, err := loader.LoadByName(pkg.Name)
			if err != nil {
				Debugf("skipping %s: no longer in any tap (%v)\n", pkg.Name, err)
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
		lnk.Unlink(t.formula.Name)
		Logf("    Unlinked old version %s\n", t.installedVersion)

		// Install new version (old keg stays until we confirm success)
		if err := installFormula(t.formula, paths, cel, lnk, dl); err != nil {
			return err
		}

		// Remove old version keg if different from new
		oldKeg := cel.KegPath(t.formula.Name, t.installedVersion)
		if t.installedVersion != t.formula.Version {
			if err := removeDir(oldKeg); err != nil {
				Logf("    Warning: could not remove old keg %s: %v\n", oldKeg, err)
			} else {
				Logf("    Removed old keg: %s\n", oldKeg)
			}
		}
	}

	return nil
}

func runOutdated(args []string) error {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	cel := &cellar.Cellar{Path: paths.Cellar}

	installed, err := cel.List()
	if err != nil {
		return err
	}
	if len(installed) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}

	found := false
	for _, pkg := range installed {
		f, err := loader.LoadByName(pkg.Name)
		if err != nil {
			Debugf("skipping %s: not in any tap (%v)\n", pkg.Name, err)
			continue
		}
		if pkg.Version != f.Version {
			fmt.Printf("%-20s %s -> %s\n", pkg.Name, pkg.Version, f.Version)
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
