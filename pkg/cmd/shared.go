package cmd

import (
	"fmt"
	"os"

	"github.com/homegrew/grew/pkg/cache"
	"github.com/homegrew/grew/pkg/cellar"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/ui"
)

type CleanupOpts = cellar.CleanupOpts

func RunCleanup(ctx *context.Context, args []string, opts CleanupOpts) error {
	paths := ctx.Paths
	cel := &cellar.Cellar{Path: paths.Cellar}

	cleanupPaths := cellar.CleanupPaths{
		DownloadsDir: cache.New(paths.Cache).DownloadsDir(),
		PruneDirs:    []string{paths.Bin, paths.Lib, paths.Include, paths.Share},
	}

	totalBytes, err := cel.RunCleanup(args, opts, cleanupPaths)
	if err != nil {
		return err
	}

	if totalBytes == 0 {
		fmt.Println("Already clean, nothing to do.")
	} else if opts.DryRun {
		ui.FprintArrow(os.Stderr, "Would free %s", fsutil.FormatSize(totalBytes))
	} else {
		ui.FprintArrow(os.Stderr, "Freed %s", fsutil.FormatSize(totalBytes))
	}

	return nil
}
