package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"runtime"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/formula"
)

func runCache(args []string) error {
	slog.Debug("starting cache command execution")
	fs := flag.NewFlagSet("--cache", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew --cache [options] [formula|cask ...]

Display grew's download cache. See also $HOMEGREW_CACHE.

If a <formula> or <cask> is provided, display the file or directory used to cache it.

Options:
  --os=OS       Show cache file for the given operating system.
  --arch=ARCH   Show cache file for the given CPU architecture.
  -s, --build-from-source
                Show the cache file used when building from source.
  --formula     Only show cache files for formulas.
  --cask        Only show cache files for casks.
`)
	}

	flags.Register(fs)
	osName := fs.String("os", runtime.GOOS, "Show cache file for the given operating system")
	arch := fs.String("arch", runtime.GOARCH, "Show cache file for the given CPU architecture")
	buildFromSource := fs.Bool("s", false, "Show the cache file used when building from source")
	fs.BoolVar(buildFromSource, "build-from-source", *buildFromSource, "Show the cache file used when building from source")
	onlyFormula := fs.Bool("formula", false, "Only show cache files for formulas")
	onlyCask := fs.Bool("cask", false, "Only show cache files for casks")

	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	if fs.NArg() == 0 {
		fmt.Println(ctx.Paths.Cache)
		return nil
	}

	for _, name := range fs.Args() {
		found := false
		if !*onlyCask {
			f, err := ctx.Loader.LoadByName(name)
			if err == nil {
				cachePath, err := formulaCachePath(f, ctx, *osName, *arch, *buildFromSource)
				if err == nil {
					fmt.Println(cachePath)
					found = true
				}
			}
		}

		if !found && !*onlyFormula {
			c, err := ctx.CaskLoader.LoadByName(name)
			if err == nil {
				cachePath, err := caskCachePath(c, ctx, *osName, *arch, *buildFromSource)
				if err == nil {
					fmt.Println(cachePath)
					found = true
				}
			}
		}

		if !found {
			return fmt.Errorf("formula or cask not found: %s", name)
		}
	}

	return nil
}

func formulaCachePath(f *formula.Formula, ctx *readContext, osName, arch string, buildFromSource bool) (string, error) {
	var dlURL string
	var err error

	if buildFromSource {
		dlURL, err = f.GetSourceURL()
	} else {
		dlURL, err = f.GetURLForPlatform(osName, arch)
	}

	if err != nil {
		return "", err
	}

	ext := urlExt(dlURL)
	if ext == "" && f.Install.Format != "" {
		ext = "." + f.Install.Format
	}
	filename := f.Name + "-" + f.Version + ext
	return filepath.Join(ctx.Paths.Cache, "downloads", filename), nil
}

func caskCachePath(c *cask.Cask, ctx *readContext, osName, arch string, buildFromSource bool) (string, error) {
	var dlURL string
	var err error

	if buildFromSource {
		dlURL, err = c.GetSourceURL()
	} else {
		dlURL, err = c.GetURLForPlatform(osName, arch)
	}

	if err != nil {
		return "", err
	}

	u, err := url.Parse(dlURL)
	if err != nil {
		return "", err
	}
	filename := filepath.Base(u.Path)
	return filepath.Join(ctx.Paths.Cache, "downloads", filename), nil
}
