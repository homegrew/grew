package cmd

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/homegrew/grew/internal/flags"
	"github.com/spf13/cobra"
)

var searchCask bool

var SearchCmd = &cobra.Command{
	Use:   "search [flags] <query>",
	Short: "Search formulas or casks",
	Long: `Search available formulas by name or description (case-insensitive
substring match). Installed formulas are marked with *.
With --cask, search casks instead of formulas.

Examples:
  grew search json
  grew search --cask browser`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSearch(args)
	},
}

func init() {
	SearchCmd.Flags().BoolVar(&searchCask, "cask", false, "Search casks instead of formulas.")
	rootCmd.AddCommand(SearchCmd)
}

func runSearch(args []string) error {
	slog.Debug("starting search command execution")

	if len(args) != 1 {
		return fmt.Errorf("usage: grew search [--cask] <query>")
	}
	query := strings.ToLower(args[0])

	if searchCask {
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
			name := f.Name
			if flags.Verbose && f.Tap != "" {
				name = f.Tap + "/" + f.Name
			}
			fmt.Printf("%s %-20s %s\n", marker, name, f.Description)
			found = true
		}
	}

	if !found {
		fmt.Printf("No formulas found matching %q\n", query)
	}
	return nil
}
