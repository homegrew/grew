package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
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
  --json        Print information in JSON format.
  -v, --verbose Show detailed output.
  -d, --debug   Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	isCask := fs.Bool("cask", false, "Show cask info")
	isJSON := fs.Bool("json", false, "Print information in JSON format")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	if fs.NArg() == 0 {
		if *isJSON {
			return fmt.Errorf("usage: grew info --json <formula|cask>")
		}
		return printInstallationStats(ctx)
	}

	if *isJSON {
		return runInfoJSON(ctx, fs.Args(), *isCask)
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
		if f.Tap != "" {
			fmt.Printf("From: %s\n", f.Tap)
		}
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

func runInfoJSON(ctx readContext, names []string, isCask bool) error {
	var output InfoJSONv2
	lnk := &linker.Linker{Paths: ctx.Paths}

	for _, name := range names {
		if isCask {
			c, ver, _, err := loadCaskInfoData(name)
			if err != nil {
				return err
			}
			cj := CaskJSON{
				Token:     c.Name,
				FullToken: c.Name,
				Name:      []string{c.Name},
				Desc:      c.Description,
				Homepage:  c.Homepage,
				Version:   c.Version,
				Installed: ver,
				Artifacts: []CaskArtifactJSON{
					{
						App: c.Artifacts.App,
						Bin: c.Artifacts.Bin,
					},
				},
			}
			output.Casks = append(output.Casks, cj)
		} else {
			f, err := ctx.Loader.LoadByName(name)
			if err != nil {
				return fmt.Errorf("formula not found: %s", name)
			}
			fj := FormulaJSON{
				Name:         f.Name,
				FullName:     f.Name,
				Desc:         f.Description,
				License:      f.License,
				Homepage:     f.Homepage,
				Versions:     VersionsJSON{Stable: f.Version},
				Dependencies: f.Dependencies,
				KegOnly:      f.KegOnly,
			}

			if ctx.Cellar.IsInstalled(f.Name) {
				ver, _ := ctx.Cellar.InstalledVersion(f.Name)
				fj.Installed = []InstalledPackageJSON{
					{
						Version: ver,
						Linked:  lnk.IsLinked(f.Name),
					},
				}
			}
			output.Formulae = append(output.Formulae, fj)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func printInstallationStats(ctx readContext) error {
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
