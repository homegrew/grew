package missing

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/validation"
	"github.com/spf13/cobra"
)

var missingHide string

var Command = &cobra.Command{
	Use:   "missing [--hide=<hidden>] [formula ...]",
	Short: "Check kegs for missing dependencies",
	Long: `Check the given formula kegs for missing runtime dependencies.
If no formulas are provided, check all installed kegs.
Will exit with a non-zero status if any kegs are found to be missing
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
	Command.Flags().StringVar(&missingHide, "hide", "", "Act as if none of the specified formulae are installed. hidden should be a comma-separated list of formula names.")
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
			if !validation.IsValidName(name) {
				return fmt.Errorf("invalid formula name in --hide: %q", name)
			}
			hidden[name] = true
		}
	}

	// Pre-compute all installed packages for efficient O(1) lookups.
	pkgs, err := ctx.Cellar.List()
	if err != nil {
		return err
	}
	installed := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		installed[p.Name] = true
	}

	isInstalled := func(name string) bool {
		return installed[name] && !hidden[name]
	}

	var targets []string
	userProvidedTargets := len(args) > 0
	if userProvidedTargets {
		targets = args
	} else {
		for _, p := range pkgs {
			targets = append(targets, p.Name)
		}
	}
	sort.Strings(targets)

	type missingEntry struct {
		formula string
		dep     string
	}
	var findings []missingEntry

	for _, name := range targets {
		f, err := ctx.LoadFormula(name)
		if err != nil {
			if userProvidedTargets {
				return fmt.Errorf("formula not found: %q", name)
			}
			slog.Debug("skipping target (not a formula or not found)", "name", name, "err", err)
			continue
		}
		for _, dep := range f.Dependencies {
			if !isInstalled(dep) {
				findings = append(findings, missingEntry{formula: name, dep: dep})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].formula != findings[j].formula {
			return findings[i].formula < findings[j].formula
		}
		return findings[i].dep < findings[j].dep
	})

	for _, e := range findings {
		fmt.Printf("%s: %s\n", e.formula, e.dep)
	}

	if len(findings) > 0 {
		return errors.New("missing dependencies found")
	}
	return nil
}
