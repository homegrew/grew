package uses

import (
	"fmt"
	"sort"

	"github.com/homegrew/grew/pkg/context"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "uses <formula>",
	Short: "Show installed formulae that depend on a formula",
	Long: `Show installed formulae that directly depend on the specified formula.

Examples:
  grew uses openssl
  grew uses zlib`,
	Args: cobra.ExactArgs(1),
	RunE: func(c *cobra.Command, args []string) error {
		return runUses(args)
	},
}

func runUses(args []string) error {
	target := args[0]

	ctx, err := context.New()
	if err != nil {
		return err
	}

	if _, err := ctx.LoadFormula(target); err != nil {
		return fmt.Errorf("formula not found: %s", target)
	}

	installed, err := ctx.Cellar.List()
	if err != nil {
		return err
	}

	var users []string
	seen := make(map[string]bool)
	for _, pkg := range installed {
		f, err := ctx.Loader.LoadByName(pkg.Name)
		if err != nil {
			continue
		}
		for _, dep := range f.Dependencies {
			if dep == target {
				if !seen[f.Name] {
					users = append(users, f.Name)
					seen[f.Name] = true
				}
				break
			}
		}
	}

	sort.Strings(users)
	for _, name := range users {
		fmt.Println(name)
	}

	return nil
}
