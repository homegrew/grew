package info

import (
	"github.com/homegrew/grew/internal/cask"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/receipt"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/internal/context"
)

var infoCask bool
var infoJSON bool

var Command = &cobra.Command{
	Use:     "info [formula ...]",
	Aliases: []string{"abv"},
	Short:   "Show formula or cask info",
	Long: `Show detailed information about a formula including its name, version,
description, homepage, license, installed status, dependencies, and
supported platforms. With --cask, show cask details including app artifacts.

Examples:
  grew info jq
  grew info --cask firefox`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInfo(args)
	},
}

func init() {
	Command.Flags().BoolVar(&infoCask, "cask", false, "Show cask info")
	Command.Flags().BoolVar(&infoJSON, "json", false, "Print information in JSON format")
}

func runInfo(args []string) error {
	slog.Debug("starting info command execution")

	ctx, err := context.New()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		if infoJSON {
			return fmt.Errorf("usage: grew info --json <formula|cask>")
		}
		return printInstallationStats(ctx)
	}

	if infoJSON {
		return runInfoJSON(ctx, args, infoCask)
	}

	if infoCask {
		for i, name := range args {
			if i > 0 {
				fmt.Println()
			}
			if err := cask.PrintInfo(ctx.CaskLoader, ctx.Caskroom, name); err != nil {
				return err
			}
		}
		return nil
	}

	lnk := &linker.Linker{Paths: ctx.Paths}

	for i, name := range args {
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

			if cellarPath, err := ctx.Cellar.KegPath(f.Name, ver); err == nil {
				if r, err := receipt.Load(cellarPath); err == nil {
					if r.PouredFromBottle {
						fmt.Printf("  Poured from bottle on %s\n", r.InstalledAt.Format("2006-01-02 at 15:04:05"))
					} else {
						fmt.Printf("  Built from source on %s\n", r.InstalledAt.Format("2006-01-02 at 15:04:05"))
					}
				}
			}
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

func runInfoJSON(ctx *context.Context, names []string, isCask bool) error {
	var output InfoJSONv2
	lnk := &linker.Linker{Paths: ctx.Paths}

	for _, name := range names {
		if isCask {
			c, ver, err := cask.LoadInfoData(ctx.CaskLoader, ctx.Caskroom, name)
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
				ip := InstalledPackageJSON{
					Version: ver,
					Linked:  lnk.IsLinked(f.Name),
				}
				if cellarPath, err := ctx.Cellar.KegPath(f.Name, ver); err == nil {
					if r, err := receipt.Load(cellarPath); err == nil {
						ip.BuiltFromSource = r.BuiltFromSource
						ip.InstalledAt = r.InstalledAt.Format(time.RFC3339)
					}
				}
				fj.Installed = []InstalledPackageJSON{ip}
			}
			output.Formulae = append(output.Formulae, fj)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func printInstallationStats(ctx *context.Context) error {
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
