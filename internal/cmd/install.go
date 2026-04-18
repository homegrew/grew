package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/depgraph"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/logger"
	"github.com/homegrew/grew/internal/relocation"
	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/signing"
	"github.com/homegrew/grew/internal/snapshot"
	"github.com/homegrew/grew/pkg/safepath"
)

func RunInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.Register(fs)
	isCask := fs.Bool("cask", false, "Install a macOS application cask")
	buildFromSource := fs.Bool("s", false, "Build from source")
	fs.BoolVar(buildFromSource, "build-from-source", false, "Build from source")
	onlyDeps := fs.Bool("only-dependencies", false, "Install dependencies only")
	ignoreDeps := fs.Bool("ignore-dependencies", false, "Skip dependency installation")
	skipPostInstall := fs.Bool("skip-post-install", false, "Skip post-install steps")
	skipLink := fs.Bool("skip-link", false, "Do not create symlinks")
	requireSHA := fs.Bool("require-sha", false, "Refuse if SHA256 is missing")
	dryRun := fs.Bool("n", false, "Dry run: show what would be installed without doing it")
	fs.BoolVar(dryRun, "dry-run", false, "Dry run: show what would be installed without doing it")
	noQuarantine := fs.Bool("no-quarantine", false, "Skip quarantine attribute on cask apps (not recommended)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

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
		return caskInstall(remaining[0], *noQuarantine)
	}

	name := remaining[0]

	ctx, err := newInstallContext()
	if err != nil {
		return err
	}

	var installOrder []*formula.Formula
	if *ignoreDeps {
		f, err := ctx.Loader.LoadByName(name)
		if err != nil {
			return fmt.Errorf("formula not found: %s", name)
		}
		installOrder = []*formula.Formula{f}
	} else {
		resolver := &depgraph.Resolver{Loader: ctx.Loader}
		slog.Debug(fmt.Sprintf("resolving dependencies for %s", name))
		var err error
		installOrder, err = resolver.Resolve(name)
		if err != nil {
			return err
		}
		slog.Debug(fmt.Sprintf("resolved %d formula(s)", len(installOrder)))
	}

	if flags.Verbose && len(installOrder) > 1 {
		names := make([]string, len(installOrder))
		for i, f := range installOrder {
			names[i] = f.Name
		}
		slog.Info("install order: " + fmt.Sprintf("%v", names))
	}

	if *requireSHA {
		for _, f := range installOrder {
			if *onlyDeps && f.Name == name {
				continue
			}
			if ctx.Cellar.IsInstalled(f.Name) {
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

	if *dryRun {
		return simulateInstall(installOrder, name, ctx, *onlyDeps, *buildFromSource)
	}

	for _, f := range installOrder {
		if *onlyDeps && f.Name == name {
			continue
		}

		if ctx.Cellar.IsInstalled(f.Name) {
			fmt.Printf("==> %s %s is already installed, skipping\n", f.Name, f.Version)
			continue
		}

		opts := installOpts{
			skipPostInstall:    *skipPostInstall,
			skipLink:           *skipLink,
			installedOnRequest: f.Name == name,
		}
		if *buildFromSource && f.Name == name {
			if err := installFormulaFromSource(f, ctx, opts); err != nil {
				return err
			}
		} else {
			if err := installFormula(f, ctx, opts); err != nil {
				return err
			}
		}
	}

	return nil
}

// simulateInstall prints what would happen without making any changes.
func simulateInstall(installOrder []*formula.Formula, target string, ctx *installContext, onlyDeps bool, buildFromSource bool) error {
	fmt.Printf("==> Dry run: the following actions would be performed\n\n")

	for _, f := range installOrder {
		if onlyDeps && f.Name == target {
			continue
		}

		if ctx.Cellar.IsInstalled(f.Name) {
			fmt.Printf("  skip      %s %s (already installed)\n", f.Name, f.Version)
			continue
		}

		method := "bottle"
		if buildFromSource && f.Name == target {
			method = "source"
		}

		dlURL := ""
		sha := ""
		if method == "source" {
			dlURL, _ = f.GetSourceURL()
			sha, _ = f.GetSourceSHA256()
		} else {
			dlURL, _ = f.GetURL()
			sha, _ = f.GetSHA256()
		}

		action := "install"
		if f.Name != target {
			action = "dep"
		}

		fmt.Printf("  %-9s %s %s (%s)\n", action, f.Name, f.Version, method)

		if flags.Verbose {
			if dlURL != "" {
				fmt.Printf("            url:    %s\n", dlURL)
			}
			if sha != "" {
				fmt.Printf("            sha256: %s\n", sha)
			}
			kegPath, _ := ctx.Cellar.KegPath(f.Name, f.Version)
			fmt.Printf("            keg:    %s\n", kegPath)
			if f.KegOnly {
				fmt.Printf("            link:   keg-only (not linked)\n")
			} else {
				fmt.Printf("            link:   opt/%s -> %s\n", f.Name, kegPath)
			}
			if len(f.Dependencies) > 0 {
				fmt.Printf("            deps:   %s\n", strings.Join(f.Dependencies, ", "))
			}
			if f.PostInstall != "" {
				fmt.Printf("            post:   yes\n")
			}
		}
	}

	return nil
}

// installOpts controls behavior of installFormula and installFormulaFromSource.
type installOpts struct {
	skipPostInstall    bool
	skipLink           bool
	installedOnRequest bool // true if the user asked for this formula directly
}

// installFormula downloads, verifies, extracts, and links a single formula.
// Shared by install and upgrade commands.
func installFormula(f *formula.Formula, ctx *installContext, opts installOpts) error {
	paths := ctx.Paths
	defer logger.TimeOp(fmt.Sprintf("install %s %s", f.Name, f.Version))()
	slog.Debug(fmt.Sprintf("platform: %s, install type: %s, keg_only: %v", formula.PlatformKey(), f.Install.Type, f.KegOnly))
	fmt.Printf("==> Installing %s %s\n", f.Name, f.Version)

	dlURL, err := f.GetURL()
	if err != nil {
		return err
	}
	slog.Info("URL: " + dlURL)

	sha, err := f.GetSHA256()
	if err != nil {
		return err
	}
	slog.Info("expected SHA256: " + sha)

	// Validate formula-derived identifiers before using them in filesystem paths.
	if err := safepath.SafePathComponent(f.Name); err != nil {
		return fmt.Errorf("invalid formula name: %w", err)
	}
	if err := safepath.SafePathComponent(f.Version); err != nil {
		return fmt.Errorf("invalid formula version: %w", err)
	}

	ext := urlExt(dlURL)
	if ext == "" && f.Install.Format != "" {
		ext = "." + f.Install.Format
	}
	filename := f.Name + "-" + f.Version + ext
	if err := safepath.SafePathComponent(filename); err != nil {
		return fmt.Errorf("invalid download filename: %w", err)
	}
	localFile, err := ctx.DL.Download(dlURL, filename)
	if err != nil {
		return fmt.Errorf("download %s: %w", f.Name, err)
	}
	slog.Info("saved to: " + localFile)

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

	fmt.Printf("==> Extracting (sandboxed)\n")
	if err := sandboxedExtract(localFile, stageDir, f.Install); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("extract %s: %w", f.Name, err)
	}
	slog.Info("extracted to staging: " + stageDir)

	kegPath, err := ctx.Cellar.KegPath(f.Name, f.Version)
	if err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("keg path %s: %w", f.Name, err)
	}
	if err := ctx.Cellar.Install(f.Name, f.Version, stageDir); err != nil {
		os.RemoveAll(stageDir)
		os.Remove(localFile)
		return fmt.Errorf("cellar install %s: %w", f.Name, err)
	}
	slog.Info("installed to cellar: " + kegPath)

	// Relocate hardcoded library paths from CI build prefix to local prefix.
	if relErr := relocation.RelocateKeg(kegPath, paths.Root); relErr != nil {
		return fmt.Errorf("relocate %s: %w", f.Name, relErr)
	}

	if !opts.skipLink {
		if err := ctx.Linker.Link(f.Name, f.Version, f.KegOnly); err != nil {
			return fmt.Errorf("link %s: %w", f.Name, err)
		}
		slog.Info(fmt.Sprintf("linked: opt/%s -> %s", f.Name, kegPath))
	}

	// Verify that relocated binaries can resolve their dependencies.
	if issues := relocation.VerifyKeg(kegPath, paths.Root); len(issues) > 0 {
		for _, issue := range issues {
			slog.Warn(fmt.Sprintf("linkage issue: %s", issue))
		}
		return fmt.Errorf("linkage verification failed for %s: %d issue(s) (use -d for details)", f.Name, len(issues))
	}

	// Capture and save integrity snapshot.
	meta := snapshot.InstallMeta{
		Platform:           formula.PlatformKey(),
		DownloadURL:        dlURL,
		DownloadSHA256:     sha,
		Dependencies:       f.Dependencies,
		InstalledOnRequest: opts.installedOnRequest,
		BuiltFromSource:    false,
	}
	manifest, snapErr := snapshot.Capture(f.Name, f.Version, kegPath, meta)
	if snapErr != nil {
		slog.Warn(fmt.Sprintf("could not capture snapshot: %v", snapErr))
	} else {
		if err := snapshot.Save(manifest, kegPath); err != nil {
			slog.Warn(fmt.Sprintf("could not save snapshot: %v", err))
		}
		slog.Info(fmt.Sprintf("snapshot saved: %s/%s", kegPath, snapshot.ManifestFile))
	}

	os.RemoveAll(stageDir)
	os.Remove(localFile)

	if err := runPostInstall(f, kegPath, opts.skipPostInstall); err != nil {
		return err
	}

	if ctx.AuditLog != nil {
		ctx.AuditLog.Log(auditlog.ActionInstall, f.Name, f.Version, sha, "bottle")
	}

	if f.KegOnly {
		fmt.Printf("==> %s %s installed (keg-only, not linked)\n", f.Name, f.Version)
	} else if opts.skipLink {
		fmt.Printf("==> %s %s installed (linking skipped)\n", f.Name, f.Version)
	} else {
		fmt.Printf("==> %s %s installed and linked\n", f.Name, f.Version)
	}
	return nil
}

// installFormulaFromSource downloads the source tarball and builds from source
// inside a sandboxed environment (no network, restricted filesystem access).
func installFormulaFromSource(f *formula.Formula, ctx *installContext, opts installOpts) error {
	paths := ctx.Paths
	defer logger.TimeOp(fmt.Sprintf("build from source %s %s", f.Name, f.Version))()

	if err := safepath.SafePathComponent(f.Name); err != nil {
		return fmt.Errorf("invalid formula name: %w", err)
	}
	if err := safepath.SafePathComponent(f.Version); err != nil {
		return fmt.Errorf("invalid formula version: %w", err)
	}

	fmt.Printf("==> Building %s %s from source\n", f.Name, f.Version)

	srcURL, err := f.GetSourceURL()
	if err != nil {
		return err
	}
	slog.Info("source URL: " + srcURL)

	srcSHA, err := f.GetSourceSHA256()
	if err != nil {
		return err
	}
	slog.Info("expected SHA256: " + srcSHA)

	ext := urlExt(srcURL)
	filename := f.Name + "-" + f.Version + "-src" + ext
	localFile, err := ctx.DL.Download(srcURL, filename)
	if err != nil {
		return fmt.Errorf("download source %s: %w", f.Name, err)
	}
	slog.Info("saved to: " + localFile)

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
	fmt.Printf("==> Extracting source (sandboxed)\n")
	if err := sandboxedExtract(localFile, buildDir, srcSpec); err != nil {
		os.RemoveAll(buildDir)
		os.Remove(localFile)
		return fmt.Errorf("extract source %s: %w", f.Name, err)
	}
	slog.Info("extracted source to: " + buildDir)

	// Prepare keg directory.
	kegPath, err := ctx.Cellar.KegPath(f.Name, f.Version)
	if err != nil {
		os.RemoveAll(buildDir)
		os.Remove(localFile)
		return fmt.Errorf("keg path %s: %w", f.Name, err)
	}
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
	slog.Debug(fmt.Sprintf("sandbox config: build=%s keg=%s deps=%v", buildDir, kegPath, depPaths))

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

	if !opts.skipLink {
		if err := ctx.Linker.Link(f.Name, f.Version, f.KegOnly); err != nil {
			return fmt.Errorf("link %s: %w", f.Name, err)
		}
		slog.Info(fmt.Sprintf("linked: opt/%s -> %s", f.Name, kegPath))
	}

	cleanup()

	if err := runPostInstall(f, kegPath, opts.skipPostInstall); err != nil {
		return err
	}

	if ctx.AuditLog != nil {
		ctx.AuditLog.Log(auditlog.ActionInstall, f.Name, f.Version, srcSHA, "source")
	}

	if f.KegOnly {
		fmt.Printf("==> %s %s built from source and installed (keg-only, not linked)\n", f.Name, f.Version)
	} else if opts.skipLink {
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
		slog.Warn(fmt.Sprintf("%s has no signature (trusted keys are configured)", name))
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
