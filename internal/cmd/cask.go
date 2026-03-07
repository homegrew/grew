package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/tap"
)

func newCaskLoader(tapDir string) *cask.Loader {
	l := &cask.Loader{TapDir: tapDir}
	if Debug {
		l.DebugLog = func(format string, args ...any) {
			Debugf(format, args...)
		}
	}
	return l
}

func initCaskTap(paths config.Paths) error {
	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	return tapMgr.InitCask()
}

func caskInstall(name string) error {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}
	if err := initCaskTap(paths); err != nil {
		return fmt.Errorf("init cask tap: %w", err)
	}

	loader := newCaskLoader(paths.Taps)
	c, err := loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("cask not found: %s", name)
	}

	cr := &cask.Caskroom{Path: paths.Caskroom}
	if cr.IsInstalled(c.Name) {
		fmt.Printf("==> %s %s is already installed, skipping\n", c.Name, c.Version)
		return nil
	}

	defer TimeOp(fmt.Sprintf("install cask %s %s", c.Name, c.Version))()
	Debugf("platform: %s\n", formula.PlatformKey())
	fmt.Printf("==> Installing cask %s %s\n", c.Name, c.Version)

	dlURL, err := c.GetURL()
	if err != nil {
		return err
	}
	Logf("    URL: %s\n", dlURL)

	sha, err := c.GetSHA256()
	if err != nil {
		return err
	}
	Logf("    Expected SHA256: %s\n", sha)

	dl := &downloader.Downloader{TmpDir: paths.Tmp}
	filename := c.Name + "-" + c.Version + caskURLExt(dlURL)
	localFile, err := dl.Download(dlURL, filename)
	if err != nil {
		return fmt.Errorf("download %s: %w", c.Name, err)
	}
	Logf("    Saved to: %s\n", localFile)

	if err := downloader.VerifySHA256(localFile, sha); err != nil {
		os.Remove(localFile)
		return fmt.Errorf("verify %s: %w", c.Name, err)
	}
	fmt.Printf("==> SHA256 verified\n")

	// Extract archive to staging
	stageDir := filepath.Join(paths.Tmp, c.Name+"-"+c.Version+"-cask-stage")
	os.RemoveAll(stageDir)

	spec := formula.InstallSpec{Type: "archive", StripComponents: 0}
	if err := downloader.Extract(localFile, stageDir, spec); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("extract %s: %w", c.Name, err)
	}
	Logf("    Extracted to staging: %s\n", stageDir)

	inst := &cask.Installer{AppDir: paths.AppDir, BinDir: paths.Bin}

	// Install .app artifacts
	for _, appName := range c.Artifacts.App {
		dest, err := inst.InstallApp(stageDir, appName)
		if err != nil {
			os.RemoveAll(stageDir)
			os.Remove(localFile)
			return fmt.Errorf("install artifact %s: %w", appName, err)
		}
		fmt.Printf("==> Installed %s to %s\n", appName, dest)
	}

	// Link bin artifacts
	for _, binName := range c.Artifacts.Bin {
		// Look for binary inside the .app bundle or staging dir
		binTarget := findCaskBinary(paths.AppDir, c.Artifacts.App, binName)
		if binTarget != "" {
			if err := inst.LinkBin(binName, binTarget); err != nil {
				Logf("    Warning: could not link binary %s: %v\n", binName, err)
			} else {
				Logf("    Linked binary: %s -> %s\n", binName, binTarget)
			}
		}
	}

	// Record installation
	if err := cr.Record(c.Name, c.Version); err != nil {
		return fmt.Errorf("record cask installation: %w", err)
	}

	os.RemoveAll(stageDir)
	os.Remove(localFile)

	fmt.Printf("==> %s %s installed\n", c.Name, c.Version)
	return nil
}

func caskUninstall(name string) error {
	paths := config.Default()
	if err := initCaskTap(paths); err != nil {
		return fmt.Errorf("init cask tap: %w", err)
	}

	cr := &cask.Caskroom{Path: paths.Caskroom}
	if !cr.IsInstalled(name) {
		return fmt.Errorf("cask %q is not installed", name)
	}

	loader := newCaskLoader(paths.Taps)
	c, err := loader.LoadByName(name)

	inst := &cask.Installer{AppDir: paths.AppDir, BinDir: paths.Bin}

	// Remove app artifacts
	if err == nil {
		for _, appName := range c.Artifacts.App {
			fmt.Printf("==> Removing %s...\n", appName)
			if err := inst.UninstallApp(appName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", appName, err)
			}
		}
		for _, binName := range c.Artifacts.Bin {
			inst.UnlinkBin(binName)
		}
	}

	if err := cr.Remove(name); err != nil {
		return err
	}

	fmt.Printf("==> %s uninstalled\n", name)
	return nil
}

func caskList() error {
	paths := config.Default()
	cr := &cask.Caskroom{Path: paths.Caskroom}

	casks, err := cr.List()
	if err != nil {
		return err
	}
	if len(casks) == 0 {
		fmt.Println("No casks installed.")
		return nil
	}
	for _, c := range casks {
		fmt.Printf("%-20s %s\n", c.Name, c.Version)
	}
	return nil
}

func caskInfo(name string) error {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}
	if err := initCaskTap(paths); err != nil {
		return fmt.Errorf("init cask tap: %w", err)
	}

	loader := newCaskLoader(paths.Taps)
	c, err := loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("cask not found: %s", name)
	}

	cr := &cask.Caskroom{Path: paths.Caskroom}

	fmt.Printf("%s: %s %s (cask)\n", c.Name, c.Description, c.Version)
	fmt.Printf("Homepage: %s\n", c.Homepage)
	fmt.Printf("License:  %s\n", c.License)

	if cr.IsInstalled(c.Name) {
		ver, _ := cr.InstalledVersion(c.Name)
		fmt.Printf("Installed: %s\n", ver)
	} else {
		fmt.Println("Installed: no")
	}

	if len(c.Artifacts.App) > 0 {
		fmt.Printf("Apps: %s\n", strings.Join(c.Artifacts.App, ", "))
	}
	if len(c.Artifacts.Bin) > 0 {
		fmt.Printf("Binaries: %s\n", strings.Join(c.Artifacts.Bin, ", "))
	}

	platforms := make([]string, 0, len(c.URL))
	for k := range c.URL {
		platforms = append(platforms, k)
	}
	fmt.Printf("Platforms: %s\n", strings.Join(platforms, ", "))

	return nil
}

func caskSearch(query string) error {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}
	if err := initCaskTap(paths); err != nil {
		return fmt.Errorf("init cask tap: %w", err)
	}

	loader := newCaskLoader(paths.Taps)
	all, err := loader.LoadAll()
	if err != nil {
		return err
	}

	cr := &cask.Caskroom{Path: paths.Caskroom}
	found := false
	q := strings.ToLower(query)

	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Name), q) ||
			strings.Contains(strings.ToLower(c.Description), q) {
			marker := " "
			if cr.IsInstalled(c.Name) {
				marker = "*"
			}
			fmt.Printf("%s %-20s %s (cask)\n", marker, c.Name, c.Description)
			found = true
		}
	}

	if !found {
		fmt.Printf("No casks found matching %q\n", query)
	}
	return nil
}

// findCaskBinary looks for a binary inside a .app bundle's MacOS directory.
func findCaskBinary(appDir string, apps []string, binName string) string {
	for _, appName := range apps {
		candidate := filepath.Join(appDir, appName, "Contents", "MacOS", binName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		// Also check Contents/Resources
		candidate = filepath.Join(appDir, appName, "Contents", "Resources", binName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func caskURLExt(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := filepath.Base(u.Path)
	if idx := strings.Index(base, ".tar."); idx != -1 {
		return base[idx:]
	}
	ext := filepath.Ext(base)
	if ext == "" {
		return ".zip" // default for casks
	}
	return ext
}
