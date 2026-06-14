package casks

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/homegrew/grew/pkg/cask"
	"github.com/homegrew/grew/pkg/context"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "casks",
	Short: "List all locally installable casks",
	Long: `List all casks available in locally tapped repositories.
Each cask is shown with its short name (token) and description.

Examples:
  grew casks`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := context.New()
		if err != nil {
			return err
		}
		return runCasks(ctx)
	},
}

func runCasks(ctx *context.Context) error {
	slog.Debug("starting casks command execution")
	casks, err := ctx.CaskLoader.LoadAll()
	if err != nil {
		return err
	}
	if len(casks) == 0 {
		fmt.Println("No casks available.")
		return nil
	}
	sort.Slice(casks, func(i, j int) bool {
		return casks[i].Name < casks[j].Name
	})
	for _, c := range dedupe(casks) {
		fmt.Printf("%-25s %s\n", c.Name, c.Description)
	}
	return nil
}

// dedupe removes duplicate cask names, keeping the first occurrence
// (tap priority is preserved by LoadAll's walk order).
func dedupe(casks []*cask.Cask) []*cask.Cask {
	seen := make(map[string]bool, len(casks))
	out := casks[:0]
	for _, c := range casks {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	return out
}
