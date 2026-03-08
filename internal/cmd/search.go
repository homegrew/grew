package cmd

import (
	"flag"
	"fmt"
	"strings"
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

	if *isCask {
		return caskSearch(query)
	}

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	all, err := ctx.Loader.LoadAll()
	if err != nil {
		return err
	}

	found := false

	for _, f := range all {
		if strings.Contains(strings.ToLower(f.Name), query) ||
			strings.Contains(strings.ToLower(f.Description), query) {
			marker := " "
			if ctx.Cellar.IsInstalled(f.Name) {
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
