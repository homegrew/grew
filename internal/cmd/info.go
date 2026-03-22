package cmd

import (
	"flag"
	"fmt"
	"strings"

	"github.com/homegrew/grew/internal/linker"
)

func runInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	isCask := fs.Bool("cask", false, "Show cask info")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 1 {
		return fmt.Errorf("usage: grew info [--cask] <formula>")
	}

	if *isCask {
		return caskInfo(fs.Arg(0))
	}

	name := fs.Arg(0)

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	f, err := ctx.Loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	lnk := &linker.Linker{Paths: ctx.Paths}

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
		Logf("Cellar:    %s\n", cellarPath)
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

	return nil
}
