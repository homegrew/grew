package cmd

import (
	"fmt"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
)

func runInfo(args []string) error {
	isCask := false
	var remaining []string
	for _, a := range args {
		if a == "--cask" {
			isCask = true
		} else {
			remaining = append(remaining, a)
		}
	}

	if len(remaining) != 1 {
		return fmt.Errorf("usage: grew info [--cask] <formula>")
	}

	if isCask {
		return caskInfo(remaining[0])
	}

	name := remaining[0]
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	loader := newLoader(paths.Taps)
	f, err := loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}

	fmt.Printf("%s: %s %s\n", f.Name, f.Description, f.Version)
	fmt.Printf("Homepage: %s\n", f.Homepage)
	fmt.Printf("License:  %s\n", f.License)

	if cel.IsInstalled(f.Name) {
		ver, _ := cel.InstalledVersion(f.Name)
		linked := "not linked"
		if lnk.IsLinked(f.Name) {
			linked = "linked"
		}
		fmt.Printf("Installed: %s (%s)\n", ver, linked)
		Logf("Cellar:    %s\n", cel.KegPath(f.Name, ver))
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
