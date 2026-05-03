package cmd

import (
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
	"github.com/homegrew/grew/internal/relocation"
	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/signing"
	"github.com/homegrew/grew/internal/snapshot"
	"github.com/homegrew/grew/internal/tap"
	"github.com/homegrew/grew/pkg/logger"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	installCask             bool
	installBuildFromSource  bool
	installForce            bool
	installOnlyDependencies bool
	installIgnoreDeps       bool
	installSkipPostInstall  bool
	installSkipLink         bool
	installRequireSHA       bool
	installDryRun           bool
	installNoQuarantine     bool
)

var InstallCmd = &cobra.Command{
	Use:     "install [flags] <formula>",
	Aliases: []string{"i"},
	Short:   "Install formulas or casks",
	Long: `Install a formula and its dependencies. Downloads the package, verifies
its SHA256 checksum, extracts it to the Cellar, and creates symlinks.

If the formula/cask is already installed (without --force), the command is a no-op.

Examples:
  grew install jq
  grew install -s ldns
  grew install --only-dependencies ldns
  grew install --ignore-dependencies jq
  grew install --cask firefox
  grew install --cask visual-studio-code`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunInstall(args)
	},
}

func init() {
	InstallCmd.Flags().BoolVar(&installCask, "cask", false, "Install a macOS application cask instead of a formula.")
	InstallCmd.Flags().BoolVarP(&installBuildFromSource, "build-from-source", "s", false, "Build the formula from source instead of using the pre-built bottle.")
	InstallCmd.Flags().BoolVar(&installOnlyDependencies, "only-dependencies", false, "Install the dependencies but not the formula itself.")
	InstallCmd.Flags().BoolVar(&installIgnoreDeps, "ignore-dependencies", false, "Skip installing dependencies; install only the formula.")
	InstallCmd.Flags().BoolVar(&installSkipPostInstall, "skip-post-install", false, "Do not run the post-install script.")
	InstallCmd.Flags().BoolVar(&installSkipLink, "skip-link", false, "Install to the Cellar but do not create symlinks.")
	InstallCmd.Flags().BoolVarP(&installForce, "force", "f", false, "Install formulae without checking for previously installed versions. Overwrite existing files when installing casks.")
	InstallCmd.Flags().BoolVar(&installRequireSHA, "require-sha", false, "Refuse to install if a formula is missing a SHA256 checksum.")
	InstallCmd.Flags().BoolVarP(&installDryRun, "dry-run", "n", false, "Show what would be installed without doing it.")
	InstallCmd.Flags().BoolVar(&installNoQuarantine, "no-quarantine", false, "Skip quarantine attribute on cask apps (not recommended).")
	rootCmd.AddCommand(InstallCmd)
}

func RunInstall(args []string) error {
	slog.Debug("starting install command execution")
	slog.Debug("starting install command execution")

	if installOnlyDependencies && installIgnoreDeps {
		return fmt.Errorf("--only-dependencies and --ignore-dependencies are mutually exclusive")
	}

	remaining := args
	if len(remaining) == 0 {
		if installCask {
			return fmt.Errorf("usage: grew install --cask <cask>...")
		}
		return fmt.Errorf("usage: grew install [-f] [-s] [--only-dependencies|--ignore-dependencies] <formula>...")
	}

	ctx, err := newInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	if installCask {
		if installBuildFromSource {
			return fmt.Errorf("--build-from-source is not supported for casks")
		}
		if installOnlyDependencies {
			return fmt.Errorf("--only-dependencies is not supported for casks")
		}
		if installIgnoreDeps {
			return fmt.Errorf("--ignore-dependencies is not supported for casks")
		}

		var requests []downloader.DownloadRequest
		seen := make(map[string]struct{})
		for _, name := range remaining {
			c, err := ctx.CaskLoader.LoadByName(name)
			if err != nil && strings.Contains(name, "/") {
				// Attempt to auto-tap if it's a fully qualified name
				parts := strings.Split(name, "/")
				tapName := parts[0] + "/" + parts[1]
				ui.FprintArrow(os.Stdout, "Cask not found. Auto-tapping %s...", tapName)
				mgr := &tap.Manager{TapsDir: ctx.Paths.Taps}
				if tapErr := mgr.Add(tapName, ""); tapErr == nil {
					// Retry loading after tap
					c, err = ctx.CaskLoader.LoadByName(name)
				}
			}
			if err != nil {
				return fmt.Errorf("cask not found: %s", name)
			}
			if ctx.Caskroom.IsInstalled(c.Name) && !installForce {
				continue
			}

			dlURL, err := c.GetURL()
			if err != nil {
				return err
			}
			sha256, err := c.GetSHA256()
			if err != nil {
				return err
			}
			sha512 := c.GetSHA512()
			filename := c.Name + "-" + c.Version + caskURLExt(dlURL)

			if _, ok := seen[filename]; ok {
				continue
			}
			seen[filename] = struct{}{}

			if ctx.DL.Cache == nil || !ctx.DL.Cache.Exists(filename) {
				requests = append(requests, downloader.DownloadRequest{
					URL:            dlURL,
					Filename:       filename,
					ExpectedSHA256: sha256,
					ExpectedSHA512: sha512,
				})
			}
		}

		if len(requests) > 0 {
			if err := ctx.DL.BatchDownload(requests, 4); err != nil {
				return err
			}
		}

		for _, name := range remaining {
			if err := caskInstall(name, installNoQuarantine, installForce); err != nil {
				return err
			}
		}
		return nil
	}

	for _, name := range remaining {
		var installOrder []*formula.Formula
		if installIgnoreDeps {
			f, err := ctx.Loader.LoadByName(name)
			if err != nil && strings.Contains(name, "/") {
				parts := strings.Split(name, "/")
				tapName := parts[0] + "/" + parts[1]
				ui.FprintArrow(os.Stdout, "Formula not found. Auto-tapping %s...", tapName)
				mgr := &tap.Manager{TapsDir: ctx.Paths.Taps}
				if tapErr := mgr.Add(tapName, ""); tapErr == nil {
					f, err = ctx.Loader.LoadByName(name)
				}
			}
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

		if installRequireSHA {
			for _, f := range installOrder {
				if installOnlyDependencies && f.Name == name {
					continue
				}
				if ctx.Cellar.IsInstalled(f.Name) && !(installForce && f.Name == name) {
					continue
				}
				if installBuildFromSource && f.Name == name {
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

		if installDryRun {
			if err := simulateInstall(installOrder, name, ctx, installOnlyDependencies, installBuildFromSource, installForce); err != nil {
				return err
			}
			continue
		}

		var requests []downloader.DownloadRequest
		for _, f := range installOrder {
			if installOnlyDependencies && f.Name == name {
				continue
			}

			if ctx.Cellar.IsInstalled(f.Name) && !(installForce && f.Name == name) {
				continue
			}

			var dlURL, sha256, sha512, ext, filename string
			var err error
			if installBuildFromSource && f.Name == name {
				dlURL, err = f.GetSourceURL()
				if err != nil {
					return err
				}
				sha256, err = f.GetSourceSHA256()
				if err != nil {
					return err
				}
				sha512, err = f.GetSourceSHA512()
				if err != nil {
					return err
				}
				ext = urlExt(dlURL)
				filename = f.Name + "-" + f.Version + "-src" + ext
			} else {
				dlURL, err = f.GetURL()
				if err != nil {
					return err
				}
				sha256, err = f.GetSHA256()
				if err != nil {
					return err
				}
				sha512, err = f.GetSHA512()
				if err != nil {
					return err
				}
				ext = urlExt(dlURL)
				if ext == "" && f.Install.Format != "" {
					ext = "." + f.Install.Format
				}
				filename = f.Name + "-" + f.Version + ext
			}

			if dlURL != "" {
				if ctx.DL.Cache == nil || !ctx.DL.Cache.Exists(filename) {
					requests = append(requests, downloader.DownloadRequest{
						URL:            dlURL,
						Filename:       filename,
						ExpectedSHA256: sha256,
						ExpectedSHA512: sha512,
					})
				}
			}
		}

		if len(requests) > 0 {
			if err := ctx.DL.BatchDownload(requests, 4); err != nil {
				return err
			}
		}

		for _, f := range installOrder {
			if installOnlyDependencies && f.Name == name {
				continue
			}

			if ctx.Cellar.IsInstalled(f.Name) && !(installForce && f.Name == name) {
				ui.FprintArrow(os.Stderr, "%s %s is already installed, skipping", f.Name, f.Version)
				continue
			}

			opts := installOpts{
				skipPostInstall:    installSkipPostInstall,
				skipLink:           installSkipLink,
				installedOnRequest: f.Name == name,
			}
			if installBuildFromSource && f.Name == name {
				if err := installFormulaFromSource(f, ctx, opts); err != nil {
					return err
				}
			} else {
				if err := installFormula(f, ctx, opts); err != nil {
					return err
				}
			}

			// Print caveats if the explicitly requested formula has them.
			if f.Name == name && f.Caveats != "" {
				ui.FprintArrow(os.Stderr, "Caveats")
				fmt.Fprintln(os.Stderr, f.Caveats)
			}
		}
	}

	return nil
}

// simulateInstall prints what would happen without making any changes.
func simulateInstall(installOrder []*formula.Formula, target string, ctx *installContext, onlyDeps bool, buildFromSource bool, force bool) error {
	ui.FprintArrow(os.Stderr, "Dry run: the following actions would be performed\n")

	for _, f := range installOrder {
		if onlyDeps && f.Name == target {
			continue
		}

		if ctx.Cellar.IsInstalled(f.Name) && !(force && f.Name == target) {
			fmt.Printf("  skip      %s %s (already installed)\n", f.Name, f.Version)
			continue
		}

		method := "bottle"
		if buildFromSource && f.Name == target {
			method = "source"
		}

		dlURL := ""
		sha256 := ""
		sha512 := ""
		if method == "source" {
			dlURL, _ = f.GetSourceURL()
			sha256, _ = f.GetSourceSHA256()
			sha512, _ = f.GetSourceSHA512()
		} else {
			dlURL, _ = f.GetURL()
			sha256, _ = f.GetSHA256()
			sha512, _ = f.GetSHA512()
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
			if sha256 != "" {
				fmt.Printf("            sha256: %s\n", sha256)
			}
			if sha512 != "" {
				fmt.Printf("            sha512: %s\n", sha512)
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

func normalizeDir(dir, name string) (string, error) {
	cleanDir := dir
	if abs, err := filepath.Abs(cleanDir); err == nil {
		cleanDir = abs
	}
	if eval, err := filepath.EvalSymlinks(cleanDir); err == nil {
		cleanDir = eval
	}
	cleanDir = filepath.Clean(cleanDir)
	if err := safepath.SafeAbsolutePath(cleanDir); err != nil {
		return "", fmt.Errorf("invalid %s directory %q: %w", name, cleanDir, err)
	}
	return cleanDir, nil
}

func checkFormulaPathComponents(f *formula.Formula) error {
	if err := safepath.SafePathComponent(f.Name); err != nil {
		return fmt.Errorf("invalid formula name: %w", err)
	}
	if err := safepath.SafePathComponent(f.Version); err != nil {
		return fmt.Errorf("invalid formula version: %w", err)
	}
	return nil
}

// installFormula downloads, verifies, extracts, and links a single formula.
// Shared by install and upgrade commands.
// removeIfWithinAllowed removes a file if it is located within the temporary or cache directories.
func removeIfWithinAllowed(tmpDir, cacheDir, candidate string) error {
	cleanTmp, err := normalizeDir(tmpDir, "tmp")
	if err != nil {
		return err
	}
	cleanCache, err := normalizeDir(cacheDir, "cache")
	if err != nil {
		return err
	}

	cleanCandidate, err := normalizeDir(candidate, "cleanup path")
	if err != nil {
		return err
	}

	if !safepath.IsSubpath(cleanTmp, cleanCandidate) && !safepath.IsSubpath(cleanCache, cleanCandidate) {
		return fmt.Errorf("cleanup path escapes both tmp and cache directories: %q", cleanCandidate)
	}

	return os.Remove(cleanCandidate)
}

func installFormula(f *formula.Formula, ctx *installContext, opts installOpts) (err error) {
	paths := ctx.Paths
	defer logger.TimeOp(fmt.Sprintf("install %s %s", f.Name, f.Version))()
	slog.Debug(fmt.Sprintf("platform: %s, install type: %s, keg_only: %v", formula.PlatformKey(), f.Install.Type, f.KegOnly))

	defer func() {
		if err != nil {
			slog.Error("installation failed, cleaning up", "formula", f.Name, "error", err)
			_ = ctx.Linker.Unlink(f.Name)
			_ = ctx.Cellar.UninstallVersion(f.Name, f.Version)
		}
	}()

	ui.FprintArrow(os.Stderr, "Installing %s %s", f.Name, f.Version)

	dlURL, err := f.GetURL()
	if err != nil {
		return err
	}
	slog.Info("URL: " + dlURL)

	sha256, err := f.GetSHA256()
	if err != nil {
		return err
	}
	sha512, err := f.GetSHA512()
	if err != nil {
		return err
	}

	slog.Info("expected SHA256: " + sha256)
	if sha512 != "" {
		slog.Info("expected SHA512: " + sha512)
	}

	// Validate formula-derived identifiers before using them in filesystem paths.
	if err := checkFormulaPathComponents(f); err != nil {
		return err
	}

	ext := urlExt(dlURL)
	if ext == "" && f.Install.Format != "" {
		ext = "." + f.Install.Format
	}
	filename := f.Name + "-" + f.Version + ext
	if err := safepath.SafePathComponent(filename); err != nil {
		return fmt.Errorf("invalid download filename: %w", err)
	}

	// Check if the file is already cached in the tmp directory and matches SHA256.
	localFile, err := safepath.SafeJoin(paths.Tmp, filename)
	if err != nil {
		return fmt.Errorf("invalid download path: %w", err)
	}
	if _, err := os.Stat(localFile); err == nil {
		if err := downloader.VerifySHA256(localFile, sha256); err == nil {
			ui.FprintArrow(os.Stderr, "Using cached %s", filename)
		} else {
			// Hash mismatch, re-download
			localFile, err = ctx.DL.Download(dlURL, filename)
			if err != nil {
				return fmt.Errorf("download %s: %w", f.Name, err)
			}
		}
	} else {
		localFile, err = ctx.DL.Download(dlURL, filename)
		if err != nil {
			return fmt.Errorf("download %s: %w", f.Name, err)
		}
	}

	cleanRoot, err := normalizeDir(paths.Root, "install root")
	if err != nil {
		return err
	}

	cleanTmp, err := normalizeDir(paths.Tmp, "tmp")
	if err != nil {
		return err
	}
	if err := safepath.CheckSubpath(cleanRoot, cleanTmp); err != nil {
		return fmt.Errorf("temporary directory escapes install root: %w", err)
	}

	cleanCache, err := normalizeDir(paths.Cache, "cache")
	if err != nil {
		return err
	}

	cleanLocalFile, err := normalizeDir(localFile, "downloaded file path")
	if err != nil {
		return err
	}

	if !safepath.IsSubpath(cleanTmp, cleanLocalFile) && !safepath.IsSubpath(cleanCache, cleanLocalFile) {
		return fmt.Errorf("downloaded file path escapes both tmp directory %q and cache directory %q: %q", cleanTmp, cleanCache, cleanLocalFile)
	}
	localFile = cleanLocalFile
	slog.Info("saved to: " + localFile)

	if err := downloader.VerifySHA256(localFile, sha256); err != nil {
		_ = removeIfWithinAllowed(cleanTmp, cleanCache, cleanLocalFile)
		return fmt.Errorf("verify %s (SHA256): %w", f.Name, err)
	}
	ui.FprintArrow(os.Stderr, "SHA256 verified")
	if sha512 != "" {
		if err := downloader.VerifySHA512(localFile, sha512); err != nil {
			_ = removeIfWithinAllowed(cleanTmp, cleanCache, cleanLocalFile)
			return fmt.Errorf("verify %s (SHA512): %w", f.Name, err)
		}
		ui.FprintArrow(os.Stderr, "SHA512 verified")
	}

	if err := verifySignature(f.Name, sha256, f.GetSignature(), paths.Root); err != nil {
		_ = removeIfWithinAllowed(cleanTmp, cleanCache, cleanLocalFile)
		return err
	}

	stageDir, err := safepath.SafeJoin(paths.Tmp, f.Name+"-"+f.Version+"-stage")
	if err != nil {
		return fmt.Errorf("invalid stage directory: %w", err)
	}
	os.RemoveAll(stageDir)

	ui.FprintArrow(os.Stderr, "Extracting (sandboxed)")
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

	return finalizeInstall(f, ctx, finalizeOpts{
		kegPath: kegPath,
		meta: snapshot.InstallMeta{
			Platform:           formula.PlatformKey(),
			DownloadURL:        dlURL,
			DownloadSHA256:     sha256,
			DownloadSHA512:     sha512,
			Dependencies:       f.Dependencies,
			InstalledOnRequest: opts.installedOnRequest,
			BuiltFromSource:    false,
		},
		skipLink:     opts.skipLink,
		skipPostInst: opts.skipPostInstall,
		auditSHA256:  sha256,
		auditDetail:  "bottle",
		cleanup: func() {
			os.RemoveAll(stageDir)
		},
	})
}

// installFormulaFromSource downloads the source tarball and builds from source
// inside a sandboxed environment (no network, restricted filesystem access).
func installFormulaFromSource(f *formula.Formula, ctx *installContext, opts installOpts) (err error) {
	paths := ctx.Paths
	defer logger.TimeOp(fmt.Sprintf("build from source %s %s", f.Name, f.Version))()

	defer func() {
		if err != nil {
			slog.Error("installation from source failed, cleaning up", "formula", f.Name, "error", err)
			_ = ctx.Linker.Unlink(f.Name)
			_ = ctx.Cellar.UninstallVersion(f.Name, f.Version)
		}
	}()

	if err := checkFormulaPathComponents(f); err != nil {
		return err
	}

	ui.FprintArrow(os.Stderr, "Building %s %s from source", f.Name, f.Version)

	srcURL, err := f.GetSourceURL()
	if err != nil {
		return err
	}
	slog.Info("source URL: " + srcURL)

	srcSHA256, err := f.GetSourceSHA256()
	if err != nil {
		return err
	}
	srcSHA512, err := f.GetSourceSHA512()
	if err != nil {
		return err
	}

	slog.Info("expected SHA256: " + srcSHA256)
	if srcSHA512 != "" {
		slog.Info("expected SHA512: " + srcSHA512)
	}

	ext := urlExt(srcURL)
	filename := f.Name + "-" + f.Version + "-src" + ext
	if err := safepath.SafePathComponent(filename); err != nil {
		return fmt.Errorf("invalid download filename: %w", err)
	}

	// Check if the file is already cached in the tmp directory and matches SHA256.
	localFile, err := safepath.SafeJoin(paths.Tmp, filename)
	if err != nil {
		return fmt.Errorf("invalid download path: %w", err)
	}
	if _, err := os.Stat(localFile); err == nil {
		if err := downloader.VerifySHA256(localFile, srcSHA256); err == nil {
			ui.FprintArrow(os.Stderr, "Using cached %s", filename)
		} else {
			// Hash mismatch, re-download
			localFile, err = ctx.DL.Download(srcURL, filename)
			if err != nil {
				return fmt.Errorf("download source %s: %w", f.Name, err)
			}
		}
	} else {
		localFile, err = ctx.DL.Download(srcURL, filename)
		if err != nil {
			return fmt.Errorf("download source %s: %w", f.Name, err)
		}
	}

	localFile, err = normalizeDir(localFile, "downloaded source")
	if err != nil {
		return err
	}
	cleanCacheForSrc, err := normalizeDir(paths.Cache, "cache")
	if err != nil {
		return err
	}
	cleanTmpForSrc, err := normalizeDir(paths.Tmp, "tmp")
	if err != nil {
		return err
	}
	
	if !safepath.IsSubpath(cleanTmpForSrc, localFile) && !safepath.IsSubpath(cleanCacheForSrc, localFile) {
		return fmt.Errorf("downloaded source path escapes both temp directory %q and cache directory %q: %q", cleanTmpForSrc, cleanCacheForSrc, localFile)
	}
	slog.Info("saved to: " + localFile)

	if err := downloader.VerifySHA256(localFile, srcSHA256); err != nil {
		_ = removeIfWithinAllowed(cleanTmpForSrc, cleanCacheForSrc, localFile)
		return fmt.Errorf("verify source %s (SHA256): %w", f.Name, err)
	}
	ui.FprintArrow(os.Stderr, "SHA256 verified")
	if srcSHA512 != "" {
		if err := downloader.VerifySHA512(localFile, srcSHA512); err != nil {
			_ = removeIfWithinAllowed(cleanTmpForSrc, cleanCacheForSrc, localFile)
			return fmt.Errorf("verify source %s (SHA512): %w", f.Name, err)
		}
		ui.FprintArrow(os.Stderr, "SHA512 verified")
	}

	if err := verifySignature(f.Name, srcSHA256, f.GetSourceSignature(), paths.Root); err != nil {
		os.Remove(localFile)
		return err
	}

	// Extract source to a build directory.
	buildDir, err := safepath.SafeJoin(paths.Tmp, f.Name+"-"+f.Version+"-build")
	if err != nil {
		return fmt.Errorf("invalid build directory: %w", err)
	}
	os.RemoveAll(buildDir)
	srcSpec := formula.InstallSpec{Type: "archive", StripComponents: 1, Format: f.Install.Format}
	ui.FprintArrow(os.Stderr, "Extracting source (sandboxed)")
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
		depCellar, err := safepath.SafeJoin(paths.Cellar, dep)
		if err != nil {
			return fmt.Errorf("invalid dependency path: %w", err)
		}
		depOpt, err := safepath.SafeJoin(paths.Opt, dep)
		if err != nil {
			return fmt.Errorf("invalid dependency path: %w", err)
		}
		depPaths = append(depPaths, depCellar, depOpt)
	}

	sbCfg := sandbox.BuildConfig{
		BuildDir: buildDir,
		KegDir:   kegPath,
		DepPaths: depPaths,
	}

	cleanup := func() {
		os.RemoveAll(buildDir)
	}
	cleanupAll := func() {
		cleanup()
		os.RemoveAll(kegPath)
	}

	ui.FprintArrow(os.Stderr, "Sandboxed build (network denied, filesystem restricted)")
	slog.Debug(fmt.Sprintf("sandbox config: build=%s keg=%s deps=%v", buildDir, kegPath, depPaths))

	// ./configure --prefix=<keg>
	ui.FprintArrow(os.Stderr, "./configure --prefix=%s", kegPath)
	configure := sandbox.Command(sbCfg, "./configure", "--prefix="+kegPath)
	configure.Dir = buildDir
	configure.Stdout = os.Stdout
	configure.Stderr = os.Stderr
	if err := configure.Run(); err != nil {
		cleanupAll()
		return fmt.Errorf("configure %s: %w", f.Name, err)
	}

	// make
	ui.FprintArrow(os.Stderr, "make")
	makeCmd := sandbox.Command(sbCfg, "make")
	makeCmd.Dir = buildDir
	makeCmd.Stdout = os.Stdout
	makeCmd.Stderr = os.Stderr
	if err := makeCmd.Run(); err != nil {
		cleanupAll()
		return fmt.Errorf("make %s: %w", f.Name, err)
	}

	// make install
	ui.FprintArrow(os.Stderr, "make install")
	makeInstall := sandbox.Command(sbCfg, "make", "install")
	makeInstall.Dir = buildDir
	makeInstall.Stdout = os.Stdout
	makeInstall.Stderr = os.Stderr
	if err := makeInstall.Run(); err != nil {
		cleanupAll()
		return fmt.Errorf("make install %s: %w", f.Name, err)
	}

	return finalizeInstall(f, ctx, finalizeOpts{
		kegPath: kegPath,
		meta: snapshot.InstallMeta{
			Platform:           formula.PlatformKey(),
			DownloadURL:        srcURL,
			DownloadSHA256:     srcSHA256,
			DownloadSHA512:     srcSHA512,
			Dependencies:       f.Dependencies,
			InstalledOnRequest: opts.installedOnRequest,
			BuiltFromSource:    true,
		},
		skipLink:     opts.skipLink,
		skipPostInst: opts.skipPostInstall,
		auditSHA256:  srcSHA256,
		auditDetail:  "source",
		cleanup:      cleanup,
	})
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
	ui.FprintArrow(os.Stderr, "Signature verified")
	return nil
}

type finalizeOpts struct {
	kegPath      string
	meta         snapshot.InstallMeta
	skipLink     bool
	skipPostInst bool
	auditSHA256  string
	auditDetail  string
	cleanup      func()
}

func finalizeInstall(f *formula.Formula, ctx *installContext, opts finalizeOpts) error {
	if !opts.skipLink {
		if err := ctx.Linker.Link(f.Name, f.Version, f.KegOnly); err != nil {
			return fmt.Errorf("link %s: %w", f.Name, err)
		}
		slog.Info(fmt.Sprintf("linked: opt/%s -> %s", f.Name, opts.kegPath))
	}

	if !opts.meta.BuiltFromSource {
		// Verify that relocated binaries can resolve their dependencies.
		if issues := relocation.VerifyKeg(opts.kegPath, ctx.Paths.Root); len(issues) > 0 {
			for _, issue := range issues {
				slog.Warn(fmt.Sprintf("linkage issue: %s", issue))
			}
			return fmt.Errorf("linkage verification failed for %s: %d issue(s) (use -d for details)", f.Name, len(issues))
		}
	}

	// prune share/info and share/man before snapshotting
	infoPath := filepath.Join(opts.kegPath, "share", "info")
	os.RemoveAll(infoPath)
	manPath := filepath.Join(opts.kegPath, "share", "man")
	os.RemoveAll(manPath)
	// remove share if empty
	sharePath := filepath.Join(opts.kegPath, "share")
	_ = os.Remove(sharePath) // fails if not empty

	manifest, snapErr := snapshot.Capture(f.Name, f.Version, opts.kegPath, opts.meta)
	if snapErr != nil {
		slog.Warn(fmt.Sprintf("could not capture snapshot: %v", snapErr))
	} else {
		if err := snapshot.Save(manifest, opts.kegPath); err != nil {
			slog.Warn(fmt.Sprintf("could not save snapshot: %v", err))
		}
		slog.Info(fmt.Sprintf("snapshot saved: %s/%s", opts.kegPath, snapshot.ManifestFile))
	}

	if opts.cleanup != nil {
		opts.cleanup()
	}

	if err := runPostInstall(f, opts.kegPath, opts.skipPostInst); err != nil {
		return err
	}

	if ctx.AuditLog != nil {
		ctx.AuditLog.Log(auditlog.ActionInstall, f.Name, f.Version, opts.auditSHA256, opts.auditDetail)
	}

	var methodStr string
	if opts.meta.BuiltFromSource {
		methodStr = "built from source and "
	}
	if f.KegOnly {
		ui.FprintArrow(os.Stderr, "%s %s %sinstalled (keg-only, not linked)", f.Name, f.Version, methodStr)
	} else if opts.skipLink {
		ui.FprintArrow(os.Stderr, "%s %s %sinstalled (linking skipped)", f.Name, f.Version, methodStr)
	} else {
		ui.FprintArrow(os.Stderr, "%s %s %sinstalled", f.Name, f.Version, methodStr)
	}

	return nil
}

func runPostInstall(f *formula.Formula, kegPath string, skipPostInstall bool) error {
	slog.Debug("starting postinstall command execution")
	if f.PostInstall == "" {
		return nil
	}
	if skipPostInstall {
		ui.FprintArrow(os.Stderr, "Skipping post-install step for %s", f.Name)
		return nil
	}
	ui.FprintArrow(os.Stderr, "Running post-install for %s (sandboxed, keg read-only)", f.Name)

	// Create a dedicated temp directory for the post-install script.
	// This is the ONLY writable location — the keg itself is read-only.
	// Use a constant pattern to avoid user-controlled data in path expressions.
	piTmp, err := os.MkdirTemp("", "grew-postinstall-*")
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
