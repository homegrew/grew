package cmd

import (
	"flag"
	"fmt"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
)

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	isCask := fs.Bool("cask", false, "List installed casks")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *isCask {
		return caskList()
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	packages, err := cel.List()
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}

	for _, p := range packages {
		fmt.Printf("%-20s %s\n", p.Name, p.Version)
	}
	return nil
}
