package install

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/homegrew/grew/pkg/caveats"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/depgraph"
	"github.com/homegrew/grew/pkg/downloader"
	"github.com/homegrew/grew/pkg/flags"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/installer"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	installCask             bool
	installBuildFromSource  bool
	installForce            bool
	installForceBottle      bool
	installOnlyDependencies bool
	installIgnoreDeps       bool
	installSkipPostInstall  bool
	installSkipLink         bool
	installRequireSHA       bool
	installDryRun           bool
	installNoQuarantine     bool
)

var Command = &cobra.Command{
	Use:     "install [flags] <formula>",
	Aliases: []string{"i"},
	Short:   "Install formulas or casks",
	Long: `Install a formula and its dependencies. Downloads the package, verifies
its SHA256 checksum, extracts it to the Cellar, and creates symlinks.

If the formula/cask is already installed (without --force), the command is a no-op.

Examples:
  grew install jq
  grew install -s ldns
  grew install --force-bottle jq
  grew install --only-dependencies ldns
  grew install --ignore-dependencies jq
  grew install --cask firefox
  grew install --cask visual-studio-code`,
	RunE: func(cobraCmd *cobra.Command, args []string) error {
		return RunInstall(args)
	},
}

func init() {
	Command.Flags().BoolVar(&installCask, "cask", false, "Install a macOS application cask instead of a formula.")
	Command.Flags().BoolVarP(&installBuildFromSource, "build-from-source", "s", false, "Build the formula from source instead of using the pre-built bottle.")
	Command.Flags().BoolVar(&installForceBottle, "force-bottle", false, "Install from a bottle if it exists for the current or newest version of macOS, even if it would not normally be used for installation.")
	Command.Flags().BoolVar(&installOnlyDependencies, "only-dependencies", false, "Install the dependencies but not the formula itself.")
	Command.Flags().BoolVar(&installIgnoreDeps, "ignore-dependencies", false, "Skip installing dependencies; install only the formula.")
	Command.Flags().BoolVar(&installSkipPostInstall, "skip-post-install", false, "Do not run the post-install script.")
	Command.Flags().BoolVar(&installSkipLink, "skip-link", false, "Install to the Cellar but do not create symlinks.")
	Command.Flags().BoolVarP(&installForce, "force", "f", false, "Install formulae without checking for previously installed versions. Overwrite existing files when installing casks.")
	Command.Flags().BoolVar(&installRequireSHA, "require-sha", false, "Refuse to install if a formula is missing a SHA256 checksum.")
	Command.Flags().BoolVarP(&installDryRun, "dry-run", "n", false, "Show what would be installed without doing it.")
	Command.Flags().BoolVar(&installNoQuarantine, "no-quarantine", false, "Skip quarantine attribute on cask apps (not recommended).")
}

func RunInstall(args []string) error {
	slog.Debug("starting install command execution")

	if installOnlyDependencies && installIgnoreDeps {
		return fmt.Errorf("--only-dependencies and --ignore-dependencies are mutually exclusive")
	}

	if installBuildFromSource && installForceBottle {
		return fmt.Errorf("--build-from-source and --force-bottle are mutually exclusive")
	}

	remaining := args
	if len(remaining) == 0 {
		if installCask {
			return fmt.Errorf("usage: grew install --cask <cask>...")
		}
		return fmt.Errorf("usage: grew install [-f] [-s] [--only-dependencies|--ignore-dependencies] <formula>...")
	}

	ctx, err := context.NewInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	if installCask {
		if installBuildFromSource {
			return fmt.Errorf("--build-from-source is not supported for casks")
		}
		if installForceBottle {
			return fmt.Errorf("--force-bottle is not supported for casks")
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
			c, err := ctx.LoadCask(name)
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
			filename := c.Name + "-" + c.Version + safepath.URLExt(dlURL)

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
			if err := installer.CaskInstall(ctx, name, installNoQuarantine, installForce, installSkipLink); err != nil {
				return err
			}
		}
		return nil
	}

	for _, name := range remaining {
		var installOrder []*formula.Formula
		if installIgnoreDeps {
			f, err := ctx.LoadFormula(name)
			if err != nil {
				return fmt.Errorf("formula not found: %s", name)
			}
			installOrder = []*formula.Formula{f}
		} else {
			resolver := &depgraph.Resolver{
				Loader:      ctx.Loader,
				LoadFormula: ctx.LoadFormula,
			}
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
			if err := simulateInstall(installOrder, name, ctx, installOnlyDependencies, installBuildFromSource, installForce, installForceBottle); err != nil {
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
				ext = safepath.URLExt(dlURL)
				filename = f.Name + "-" + f.Version + "-src" + ext
			} else {
				if installForceBottle && f.Name == name {
					dlURL, sha256, sha512, err = f.ResolveForceBottle()
				} else {
					dlURL, err = f.GetURL()
					if err == nil {
						sha256, err = f.GetSHA256()
					}
					if err == nil {
						sha512, err = f.GetSHA512()
					}
				}
				if err != nil {
					return err
				}
				ext = safepath.URLExt(dlURL)
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
				if !ctx.Linker.IsLinked(f.Name) {
					ui.FprintArrow(os.Stderr, "Linking %s %s...", f.Name, f.Version)
					_ = ctx.Linker.Link(f.Name, f.Version, f.EffectiveKegOnly())
				} else {
					ui.FprintArrow(os.Stderr, "%s %s is already installed and linked, skipping", f.Name, f.Version)
				}
				continue
			}

			opts := installer.InstallOpts{
				SkipPostInstall:    installSkipPostInstall,
				SkipLink:           installSkipLink && f.Name == name,
				InstalledOnRequest: f.Name == name,
				ForceBottle:        installForceBottle && f.Name == name,
			}
			if f.Name == name {
				opts.CaveatRenderer = caveats.New(os.Stderr)
			}
			if installBuildFromSource && f.Name == name {
				if err := installer.InstallFormulaFromSource(f, ctx, opts); err != nil {
					return err
				}
			} else {
				if err := installer.InstallFormula(f, ctx, opts); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// simulateInstall prints what would happen without making any changes.
func simulateInstall(installOrder []*formula.Formula, target string, ctx *context.InstallContext, onlyDeps bool, buildFromSource bool, force bool, forceBottle bool) error {
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
		} else if forceBottle && f.Name == target {
			method = "bottle (forced)"
		}

		dlURL := ""
		sha256 := ""
		sha512 := ""
		if method == "source" {
			dlURL, _ = f.GetSourceURL()
			sha256, _ = f.GetSourceSHA256()
			sha512, _ = f.GetSourceSHA512()
		} else if forceBottle && f.Name == target {
			dlURL, sha256, sha512, _ = f.ResolveForceBottle()
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
			if installSkipLink && f.Name == target {
				fmt.Printf("            link:   skipped (--skip-link)\n")
			} else if f.EffectiveKegOnly() {
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
