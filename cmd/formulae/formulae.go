package formulae

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "formulae",
	Short: "List all locally installable formulae",
	Long: `List all formulae available in locally tapped repositories.
Each formula is shown with its name and description.

Examples:
  grew formulae`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := context.New()
		if err != nil {
			return err
		}
		return runFormulae(ctx)
	},
}

func runFormulae(ctx *context.Context) error {
	slog.Debug("starting formulae command execution")
	formulae, err := ctx.Loader.LoadAll()
	if err != nil {
		return err
	}
	if len(formulae) == 0 {
		fmt.Println("No formulae available.")
		return nil
	}
	sort.Slice(formulae, func(i, j int) bool {
		return formulae[i].Name < formulae[j].Name
	})
	for _, f := range dedupe(formulae) {
		fmt.Printf("%-25s %s\n", f.Name, f.Description)
	}
	return nil
}

// dedupe removes duplicate formula names, keeping the first occurrence
// (tap priority is preserved by LoadAll's walk order).
func dedupe(formulae []*formula.Formula) []*formula.Formula {
	seen := make(map[string]bool, len(formulae))
	out := formulae[:0]
	for _, f := range formulae {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		out = append(out, f)
	}
	return out
}
