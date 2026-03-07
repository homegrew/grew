package cmd

import (
	"flag"
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
	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/signing"
	"github.com/homegrew/grew/internal/snapshot"
	"github.com/homegrew/grew/internal/tap"
)

func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	isCask := fs.Bool("cask", false, "Install a macOS application cask")
	buildFromSource := fs.Bool("s", false, "Build from source")
	fs.BoolVar(buildFromSource, "build-from-source", false, "Build from source")
	onlyDeps := fs.Bool("only-dependencies", false, "Install dependencies only")
	ignoreDeps := fs.Bool("ignore-dependencies", false, "Skip dependency installation")
	skipPostInstall := fs.Bool("skip-post-install", false, "Skip post-install steps")
	skipLink := fs.Bool("skip-link", false, "Do not create symlinks")
	requireSHA := fs.Bool("require-sha", false, "Refuse if SHA256 is missing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *onlyDeps && *ignoreDeps {
		return fmt.Errorf("--only-dependencies and --ignore-dependencies are mutually exclusive")
	}

	remaining := fs.Args()
	if len(remaining) != 1 {
		if *isCask {
			return fmt.Errorf("usage: grew install --cask <cask>")
		}
		return fmt.Errorf("usage: grew install [-s] [--only-dependencies|--ignore-dependencies] <formula>")
	}

	if *isCask {
		if *buildFromSource {
			return fmt.Errorf("--build-from-source is not supported for casks")
		}
		if *onlyDeps {
			return fmt.Errorf("--only-dependencies is not supported for casks")
		}
		if *ignoreDeps {
			return fmt.Errorf("--ignore-dependencies is not supported for casks")
		}
		return caskInstall(remaining[0])
	}

	name := remaining[0]

	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}
	dl := &downloader.Downloader{TmpDir: paths.Tmp}

	var installOrder []*formula.Formula
	if *ignoreDeps {
		f, err := loader.LoadByName(name)
		if err != nil {
			return fmt.Errorf("formula not found: %s", name)
		}
		installOrder = []*formula.Formula{f}
	} else {
		resolver := &depgraph.Resolver{Loader: loader}
		Debugf("resolving dependencies for %s\n", name)
		var err error
		installOrder, err = resolver.Resolve(name)
		if err != nil {
			return err
		}
		Debugf("resolved %d formula(s)\n", len(installOrder))
	}

	if Verbose && len(installOrder) > 1 {
		names := make([]string, len(installOrder))
		for i, f := range installOrder {
			names[i] = f.Name
		}
		Logf("==> Install order: %s\n", fmt.Sprintf("%v", names))
	}

	if *requireSHA {
		for _, f := range installOrder {
			if *onlyDeps && f.Name == name {
				continue
			}
			if cel.IsInstalled(f.Name) {
				continue
			}
			if *buildFromSource && f.Name == name {
				if _, err := f.GetSourceSHA256(); err != nil {
					return fmt.Errorf("--require-sha: %s has no source SHA256 checksum", f.Name)
				}
			} else {
				if _, err := f.GetSHA256(); err != nil {
					return fmt.Errorf("--require-sha: %s has no SHA256 checksum for platform %s", f.Name, formula.PlatformKey())
				}
			}
		}
	}

	for _, f := range installOrder {
		if *onlyDeps && f.Name == name {
			continue
		}

		if cel.IsInstalled(f.Name) {
			fmt.Printf("==> %s %s is already installed, skipping\n", f.Name, f.Version)
			continue
		}

		if *buildFromSource && f.Name == name {
			if err := installFormulaFromSource(f, paths, cel, lnk, dl, *skipPostInstall, *skipLink); err != nil {
				return err
			}
		} else {
			if err := installFormula(f, paths, cel, lnk, dl, *skipPostInstall, *skipLink); err != nil {
				return err
			}
		}
	}

	return nil
}

// installFormula downloads, verifies, extracts, and links a single formula.
// Shared by install and upgrade commands.
func installFormula(f *formula.Formula, paths config.Paths, cel *cellar.Cellar, lnk *linker.Linker, dl *downloader.Downloader, skipPostInstall bool, skipLink bool) error {
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

	ext := urlExt(dlURL)
	if ext == "" && f.Install.Format != "" {
		ext = "." + f.Install.Format
	}
	filename := f.Name + "-" + f.Version + ext
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

	if err := verifySignature(f.Name, sha, f.GetSignature(), paths.Root); err != nil {
		os.Remove(localFile)
		return err
	}

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

	if !skipLink {
		if err := lnk.Link(f.Name, f.Version, f.KegOnly); err != nil {
			return fmt.Errorf("link %s: %w", f.Name, err)
		}
		Logf("    Linked: opt/%s -> %s\n", f.Name, kegPath)
	}

	// Capture and save integrity snapshot.
	meta := snapshot.InstallMeta{
		Platform:       formula.PlatformKey(),
		DownloadURL:    dlURL,
		DownloadSHA256: sha,
		Dependencies:   f.Dependencies,
	}
	manifest, snapErr := snapshot.Capture(f.Name, f.Version, kegPath, meta)
	if snapErr != nil {
		Logf("    Warning: could not capture snapshot: %v\n", snapErr)
	} else {
		if err := snapshot.Save(manifest, kegPath); err != nil {
			Logf("    Warning: could not save snapshot: %v\n", err)
		}
		Logf("    Snapshot saved: %s/%s\n", kegPath, snapshot.ManifestFile)
	}

	os.RemoveAll(stageDir)
	os.Remove(localFile)

	if err := runPostInstall(f, kegPath, skipPostInstall); err != nil {
		return err
	}

	if f.KegOnly {
		fmt.Printf("==> %s %s installed (keg-only, not linked)\n", f.Name, f.Version)
	} else if skipLink {
		fmt.Printf("==> %s %s installed (linking skipped)\n", f.Name, f.Version)
	} else {
		fmt.Printf("==> %s %s installed and linked\n", f.Name, f.Version)
	}
	return nil
}

// installFormulaFromSource downloads the source tarball and builds from source
// inside a sandboxed environment (no network, restricted filesystem access).
func installFormulaFromSource(f *formula.Formula, paths config.Paths, cel *cellar.Cellar, lnk *linker.Linker, dl *downloader.Downloader, skipPostInstall bool, skipLink bool) error {
	defer TimeOp(fmt.Sprintf("build from source %s %s", f.Name, f.Version))()
	fmt.Printf("==> Building %s %s from source\n", f.Name, f.Version)

	srcURL, err := f.GetSourceURL()
	if err != nil {
		return err
	}
	Logf("    Source URL: %s\n", srcURL)

	srcSHA, err := f.GetSourceSHA256()
	if err != nil {
		return err
	}
	Logf("    Expected SHA256: %s\n", srcSHA)

	ext := urlExt(srcURL)
	filename := f.Name + "-" + f.Version + "-src" + ext
	localFile, err := dl.Download(srcURL, filename)
	if err != nil {
		return fmt.Errorf("download source %s: %w", f.Name, err)
	}
	Logf("    Saved to: %s\n", localFile)

	if err := downloader.VerifySHA256(localFile, srcSHA); err != nil {
		os.Remove(localFile)
		return fmt.Errorf("verify source %s: %w", f.Name, err)
	}
	fmt.Printf("==> SHA256 verified\n")

	if err := verifySignature(f.Name, srcSHA, f.GetSourceSignature(), paths.Root); err != nil {
		os.Remove(localFile)
		return err
	}

	// Extract source to a build directory.
	buildDir := filepath.Join(paths.Tmp, f.Name+"-"+f.Version+"-build")
	os.RemoveAll(buildDir)
	srcSpec := formula.InstallSpec{Type: "archive", StripComponents: 1, Format: f.Install.Format}
	if err := downloader.Extract(localFile, buildDir, srcSpec); err != nil {
		os.RemoveAll(buildDir)
		os.Remove(localFile)
		return fmt.Errorf("extract source %s: %w", f.Name, err)
	}
	Logf("    Extracted source to: %s\n", buildDir)

	// Prepare keg directory.
	kegPath := cel.KegPath(f.Name, f.Version)
	if err := os.MkdirAll(kegPath, 0755); err != nil {
		os.RemoveAll(buildDir)
		os.Remove(localFile)
		return fmt.Errorf("create keg dir: %w", err)
	}

	// Collect dependency paths for sandbox read-only access.
	var depPaths []string
	for _, dep := range f.Dependencies {
		depPaths = append(depPaths, filepath.Join(paths.Cellar, dep))
		depPaths = append(depPaths, filepath.Join(paths.Opt, dep))
	}

	sbCfg := sandbox.BuildConfig{
		BuildDir: buildDir,
		KegDir:   kegPath,
		DepPaths: depPaths,
	}

	cleanup := func() {
		os.RemoveAll(buildDir)
		os.Remove(localFile)
	}
	cleanupAll := func() {
		cleanup()
		os.RemoveAll(kegPath)
	}

	fmt.Printf("==> Sandboxed build (network denied, filesystem restricted)\n")
	Debugf("sandbox config: build=%s keg=%s deps=%v\n", buildDir, kegPath, depPaths)

	// ./configure --prefix=<keg>
	fmt.Printf("==> ./configure --prefix=%s\n", kegPath)
	configure := sandbox.Command(sbCfg, "./configure", "--prefix="+kegPath)
	configure.Dir = buildDir
	configure.Stdout = os.Stdout
	configure.Stderr = os.Stderr
	if err := configure.Run(); err != nil {
		cleanupAll()
		return fmt.Errorf("configure %s: %w", f.Name, err)
	}

	// make
	fmt.Printf("==> make\n")
	makeCmd := sandbox.Command(sbCfg, "make")
	makeCmd.Dir = buildDir
	makeCmd.Stdout = os.Stdout
	makeCmd.Stderr = os.Stderr
	if err := makeCmd.Run(); err != nil {
		cleanupAll()
		return fmt.Errorf("make %s: %w", f.Name, err)
	}

	// make install
	fmt.Printf("==> make install\n")
	makeInstall := sandbox.Command(sbCfg, "make", "install")
	makeInstall.Dir = buildDir
	makeInstall.Stdout = os.Stdout
	makeInstall.Stderr = os.Stderr
	if err := makeInstall.Run(); err != nil {
		cleanupAll()
		return fmt.Errorf("make install %s: %w", f.Name, err)
	}

	if !skipLink {
		if err := lnk.Link(f.Name, f.Version, f.KegOnly); err != nil {
			return fmt.Errorf("link %s: %w", f.Name, err)
		}
		Logf("    Linked: opt/%s -> %s\n", f.Name, kegPath)
	}

	cleanup()

	if err := runPostInstall(f, kegPath, skipPostInstall); err != nil {
		return err
	}

	if f.KegOnly {
		fmt.Printf("==> %s %s built from source and installed (keg-only, not linked)\n", f.Name, f.Version)
	} else if skipLink {
		fmt.Printf("==> %s %s built from source and installed (linking skipped)\n", f.Name, f.Version)
	} else {
		fmt.Printf("==> %s %s built from source and installed\n", f.Name, f.Version)
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

// verifySignature checks a formula's signature against trusted keys.
// If no trusted keys file exists, verification is silently skipped.
// If trusted keys exist but no signature is present, a warning is printed.
// If both exist and verification fails, an error is returned.
func verifySignature(name, sha256Hex, signatureB64, grewRoot string) error {
	trustedKeys, err := signing.LoadTrustedKeys(grewRoot)
	if err != nil {
		return fmt.Errorf("load trusted keys: %w", err)
	}
	if len(trustedKeys) == 0 {
		// No trusted keys configured — skip signature verification.
		return nil
	}
	if signatureB64 == "" {
		fmt.Printf("==> Warning: %s has no signature (trusted keys are configured)\n", name)
		return nil
	}
	if !signing.VerifyAny(trustedKeys, sha256Hex, signatureB64) {
		return fmt.Errorf("signature verification failed for %s: not signed by any trusted key", name)
	}
	fmt.Printf("==> Signature verified\n")
	return nil
}

func runPostInstall(f *formula.Formula, kegPath string, skipPostInstall bool) error {
	if f.PostInstall == "" {
		return nil
	}
	if skipPostInstall {
		fmt.Printf("==> Skipping post-install step for %s\n", f.Name)
		return nil
	}
	fmt.Printf("==> Running post-install for %s (sandboxed, keg read-only)\n", f.Name)

	// Create a dedicated temp directory for the post-install script.
	// This is the ONLY writable location — the keg itself is read-only.
	piTmp, err := os.MkdirTemp("", fmt.Sprintf("grew-postinstall-%s-*", f.Name))
	if err != nil {
		return fmt.Errorf("create post-install tmpdir: %w", err)
	}
	defer os.RemoveAll(piTmp)

	piCfg := sandbox.PostInstallConfig{
		KegDir: kegPath,
		TmpDir: piTmp,
	}
	cmd := sandbox.PostInstallCommand(piCfg, "sh", "-c", f.PostInstall)
	cmd.Dir = kegPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("post-install failed: %w", err)
	}
	return nil
}
