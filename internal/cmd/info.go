package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"strings"

	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/internal/linker"
)

func runInfo(args []string) error {
	slog.Debug("starting info command execution")
	fs := flag.NewFlagSet("info", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew info [options] [formula ...]
       grew abv [options] [formula ...]

Show brief information about formulas or casks.
If no arguments are provided, show installation statistics.

Options:
  --cask        Show cask info.
  -v, --verbose Show detailed output.
  -d, --debug   Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	isCask := fs.Bool("cask", false, "Show cask info")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	if fs.NArg() == 0 {
		return printInstallationStats(ctx)
	}

	if *isCask {
		for i, name := range fs.Args() {
			if i > 0 {
				fmt.Println()
			}
			if err := caskInfo(name); err != nil {
				return err
			}
		}
		return nil
	}

	lnk := &linker.Linker{Paths: ctx.Paths}

	for i, name := range fs.Args() {
		if i > 0 {
			fmt.Println()
		}

		f, err := ctx.Loader.LoadByName(name)
		if err != nil {
			return fmt.Errorf("formula not found: %s", name)
		}

		fmt.Printf("%s: %s %s\n", f.Name, f.Description, f.Version)
		fmt.Printf("Homepage: %s\n", f.Homepage)
		fmt.Printf("License:  %s\n", f.License)

		if ctx.Cellar.IsInstalled(f.Name) {
			ver, _ := ctx.Cellar.InstalledVersion(f.Name)
			linked := "not linked"
			if lnk.IsLinked(f.Name) {
				linked = "linked"
			}
			fmt.Printf("Installed: %s (%s)\n", ver, linked)
			cellarPath, _ := ctx.Cellar.KegPath(f.Name, ver)
			slog.Info("cellar: " + cellarPath)
		} else {
			fmt.Println("Installed: no")
		}

		if f.KegOnly {
			fmt.Println("Keg-only: yes")
		}

		if len(f.Dependencies) > 0 {
			fmt.Printf("Dependencies: %s\n", strings.Join(f.Dependencies, ", "))
		}

		platforms := make([]string, 0, len(f.URL))
		for k := range f.URL {
			platforms = append(platforms, k)
		}
		fmt.Printf("Platforms: %s\n", strings.Join(platforms, ", "))
	}

	return nil
}

func printInstallationStats(ctx *readContext) error {
	formulas, err := ctx.Cellar.List()
	if err != nil {
		return err
	}
	casks, err := ctx.Caskroom.List()
	if err != nil {
		return err
	}

	var totalSize int64
	var totalFiles int64

	// Usage from Cellar
	size, files, err := fsutil.DiskUsage(ctx.Paths.Cellar)
	if err == nil {
		totalSize += size
		totalFiles += files
	}

	// Usage from Caskroom
	size, files, err = fsutil.DiskUsage(ctx.Paths.Caskroom)
	if err == nil {
		totalSize += size
		totalFiles += files
	}

	fmt.Printf("%d formulas, %d casks, %d files, %s\n", len(formulas), len(casks), totalFiles, fsutil.FormatSize(totalSize))
	return nil
}
