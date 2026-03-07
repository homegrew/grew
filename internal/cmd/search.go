package cmd

import (
	"flag"
	"fmt"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/tap"
)

func runSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	isCask := fs.Bool("cask", false, "Search casks")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 1 {
		return fmt.Errorf("usage: grew search [--cask] <query>")
	}
	query := strings.ToLower(fs.Arg(0))

	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		return fmt.Errorf("init core tap: %w", err)
	}

	if *isCask {
		return caskSearch(query)
	}

	// Search formulas
	loader := newLoader(paths.Taps)
	all, err := loader.LoadAll()
	if err != nil {
		return err
	}

	cel := &cellar.Cellar{Path: paths.Cellar}
	found := false

	for _, f := range all {
		if strings.Contains(strings.ToLower(f.Name), query) ||
			strings.Contains(strings.ToLower(f.Description), query) {
			marker := " "
			if cel.IsInstalled(f.Name) {
				marker = "*"
			}
			fmt.Printf("%s %-20s %s\n", marker, f.Name, f.Description)
			found = true
		}
	}

	if !found {
		fmt.Printf("No formulas found matching %q\n", query)
	}
	return nil
}
