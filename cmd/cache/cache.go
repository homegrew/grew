package cache

import (
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"runtime"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/context"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/spf13/cobra"
)

var (
	cacheOS             string
	cacheArch           string
	cacheBuildFromSource bool
	cacheOnlyFormula    bool
	cacheOnlyCask       bool
)

var Command = &cobra.Command{
	Use:     "cache [formula|cask ...]",
	Aliases: []string{"--cache"},
	Short:   "Display grew's download cache",
	Long: `Display grew's download cache. See also $HOMEGREW_CACHE.

If a <formula> or <cask> is provided, display the file or directory used to cache it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunCache(args)
	},
}

func init() {
	Command.Flags().StringVar(&cacheOS, "os", runtime.GOOS, "Show cache file for the given operating system")
	Command.Flags().StringVar(&cacheArch, "arch", runtime.GOARCH, "Show cache file for the given CPU architecture")
	Command.Flags().BoolVarP(&cacheBuildFromSource, "build-from-source", "s", false, "Show the cache file used when building from source")
	Command.Flags().BoolVar(&cacheOnlyFormula, "formula", false, "Only show cache files for formulas")
	Command.Flags().BoolVar(&cacheOnlyCask, "cask", false, "Only show cache files for casks")
}

func RunCache(args []string) error {
	slog.Debug("starting cache command execution")
	
	ctx, err := context.New()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		fmt.Println(ctx.Paths.Cache)
		return nil
	}

	for _, name := range args {
		found := false
		if !cacheOnlyCask {
			f, err := ctx.Loader.LoadByName(name)
			if err == nil {
				cachePath, err := formulaCachePath(f, ctx, cacheOS, cacheArch, cacheBuildFromSource)
				if err == nil {
					fmt.Println(cachePath)
					found = true
				}
			}
		}

		if !found && !cacheOnlyFormula {
			c, err := ctx.CaskLoader.LoadByName(name)
			if err == nil {
				cachePath, err := caskCachePath(c, ctx, cacheOS, cacheArch, cacheBuildFromSource)
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


func formulaCachePath(f *formula.Formula, ctx *context.Context, osName, arch string, buildFromSource bool) (string, error) {
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

	ext := safepath.URLExt(dlURL)
	if ext == "" && f.Install.Format != "" {
		ext = "." + f.Install.Format
	}
	filename := f.Name + "-" + f.Version + ext
	return filepath.Join(ctx.Paths.Cache, "downloads", filename), nil
}

func caskCachePath(c *cask.Cask, ctx *context.Context, osName, arch string, buildFromSource bool) (string, error) {
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
