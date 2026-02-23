package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/depgraph"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
)

func runInstall(args []string) error {
	isCask := false
	var remaining []string
	for _, a := range args {
		if a == "--cask" {
			isCask = true
		} else {
			remaining = append(remaining, a)
		}
	}

	if len(remaining) != 1 {
		if isCask {
			return fmt.Errorf("usage: grew install --cask <cask>")
		}
		return fmt.Errorf("usage: grew install <formula>")
	}

	if isCask {
		return caskInstall(remaining[0])
	}

	name := remaining[0]

	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	resolver := &depgraph.Resolver{Loader: loader}

	Debugf("resolving dependencies for %s\n", name)
	installOrder, err := resolver.Resolve(name)
	if err != nil {
		return err
	}
	Debugf("resolved %d formula(s)\n", len(installOrder))

	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}
	dl := &downloader.Downloader{TmpDir: paths.Tmp}

	if Verbose && len(installOrder) > 1 {
		names := make([]string, len(installOrder))
		for i, f := range installOrder {
			names[i] = f.Name
		}
		Logf("==> Install order: %s\n", fmt.Sprintf("%v", names))
	}

	for _, f := range installOrder {
		if cel.IsInstalled(f.Name) {
			fmt.Printf("==> %s %s is already installed, skipping\n", f.Name, f.Version)
			continue
		}

		if err := installFormula(f, paths, cel, lnk, dl); err != nil {
			return err
		}
	}

	return nil
}

// installFormula downloads, verifies, extracts, and links a single formula.
// Shared by install and upgrade commands.
func installFormula(f *formula.Formula, paths config.Paths, cel *cellar.Cellar, lnk *linker.Linker, dl *downloader.Downloader) error {
	defer TimeOp(fmt.Sprintf("install %s %s", f.Name, f.Version))()
	Debugf("platform: %s, install type: %s, keg_only: %v\n", formula.PlatformKey(), f.Install.Type, f.KegOnly)
	fmt.Printf("==> Installing %s %s\n", f.Name, f.Version)

	dlURL, err := f.GetURL()
	if err != nil {
		return err
	}
	Logf("    URL: %s\n", dlURL)

	sha, err := f.GetSHA256()
	if err != nil {
		return err
	}
	Logf("    Expected SHA256: %s\n", sha)

	filename := f.Name + "-" + f.Version + urlExt(dlURL)
	localFile, err := dl.Download(dlURL, filename)
	if err != nil {
		return fmt.Errorf("download %s: %w", f.Name, err)
	}
	Logf("    Saved to: %s\n", localFile)

	if err := downloader.VerifySHA256(localFile, sha); err != nil {
		os.Remove(localFile)
		return fmt.Errorf("verify %s: %w", f.Name, err)
	}
	fmt.Printf("==> SHA256 verified\n")

	stageDir := filepath.Join(paths.Tmp, f.Name+"-"+f.Version+"-stage")
	os.RemoveAll(stageDir)

	if err := downloader.Extract(localFile, stageDir, f.Install); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}
	Logf("    Extracted to staging: %s\n", stageDir)

	kegPath := cel.KegPath(f.Name, f.Version)
	if err := cel.Install(f.Name, f.Version, stageDir); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("cellar install %s: %w", f.Name, err)
	}
	Logf("    Installed to cellar: %s\n", kegPath)

	if err := lnk.Link(f.Name, f.Version, f.KegOnly); err != nil {
		return fmt.Errorf("link %s: %w", f.Name, err)
	}
	Logf("    Linked: opt/%s -> %s\n", f.Name, kegPath)

	os.RemoveAll(stageDir)
	os.Remove(localFile)

	if f.KegOnly {
		fmt.Printf("==> %s %s installed (keg-only, not linked)\n", f.Name, f.Version)
	} else {
		fmt.Printf("==> %s %s installed and linked\n", f.Name, f.Version)
	}
	return nil
}

// urlExt extracts the file extension from a URL path (e.g. ".tar.gz", ".zip").
func urlExt(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := filepath.Base(u.Path)
	if idx := strings.Index(base, ".tar."); idx != -1 {
		return base[idx:]
	}
	return filepath.Ext(base)
}
