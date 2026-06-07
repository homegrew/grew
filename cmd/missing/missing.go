package missing

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/homegrew/grew/pkg/context"
	"github.com/spf13/cobra"
)

var missingHide string

var Command = &cobra.Command{
	Use:   "missing [--hide=<hidden>] [formula|cask ...]",
	Short: "Check kegs and casks for missing dependencies",
	Long: `Check the given formula kegs and cask installations for missing
dependencies. If no formula or cask are provided, check all kegs and casks.
Will exit with a non-zero status if any kegs or casks are found to be missing
dependencies.`,
	// A non-zero exit is this command's normal "found missing deps" signal, not
	// a usage error — suppress cobra's usage dump and duplicate error print so
	// the output is just the list of missing dependencies.
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(c *cobra.Command, args []string) error {
		return runMissing(args)
	},
}

func init() {
	Command.Flags().StringVar(&missingHide, "hide", "", "Act as if none of the specified hidden are installed. hidden should be a comma-separated list of formulae or casks.")
}

func runMissing(args []string) error {
	slog.Debug("starting missing command execution")

	ctx, err := context.New()
	if err != nil {
		return err
	}

	hidden := make(map[string]bool)
	for _, name := range strings.Split(missingHide, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			hidden[name] = true
		}
	}

	isInstalled := func(name string) bool {
		return ctx.Cellar.IsInstalled(name) && !hidden[name]
	}

	var targets []string
	if len(args) > 0 {
		targets = args
	} else {
		pkgs, err := ctx.Cellar.List()
		if err != nil {
			return err
		}
		for _, p := range pkgs {
			targets = append(targets, p.Name)
		}
		sort.Strings(targets)
	}

	type missingEntry struct {
		formula string
		dep     string
	}
	var findings []missingEntry

	for _, name := range targets {
		f, err := ctx.LoadFormula(name)
		if err != nil {
			slog.Debug("skipping target (not a formula or not found)", "name", name, "err", err)
			continue
		}
		for _, dep := range f.Dependencies {
			if !isInstalled(dep) {
				findings = append(findings, missingEntry{formula: name, dep: dep})
			}
		}
	}

	for _, e := range findings {
		fmt.Printf("%s: %s\n", e.formula, e.dep)
	}

	if len(findings) > 0 {
		return errors.New("missing dependencies found")
	}
	return nil
}
