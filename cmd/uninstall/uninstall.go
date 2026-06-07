package uninstall

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/installer"
	"github.com/spf13/cobra"
)

var (
	uninstallCask       bool
	uninstallForce      bool
	uninstallIgnoreDeps bool
)

var Command = &cobra.Command{
	Use:     "uninstall [flags] <formula>",
	Aliases: []string{"remove", "rm"},
	Short:   "Uninstall formulas or casks",
	Long: `Uninstall a formula by removing its symlinks and Cellar directory.
With --cask, removes the .app from ~/Applications and the Caskroom entry.

Examples:
  grew uninstall jq
  grew uninstall --force jq
  grew uninstall --cask firefox`,
	RunE: func(c *cobra.Command, args []string) error {
		return runUninstall(args)
	},
}

func init() {
	Command.Flags().BoolVar(&uninstallCask, "cask", false, "Uninstall a cask instead of a formula.")
	Command.Flags().BoolVarP(&uninstallForce, "force", "f", false, "Delete all installed versions of formula. Uninstall even if cask is not installed.")
	Command.Flags().BoolVar(&uninstallIgnoreDeps, "ignore-dependencies", false, "Uninstall even if the formula is required by another installed formula.")
}

func runUninstall(args []string) error {
	slog.Debug("starting uninstall command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew uninstall [-f] [--cask] <formula>...")
	}

	if uninstallCask {
		ctx, err := context.New()
		if err != nil {
			return err
		}
		for _, name := range args {
			if err := installer.CaskUninstall(ctx, name, uninstallForce); err != nil {
				return err
			}
		}
		return nil
	}

	ctx, err := context.NewInstallContext()
	if err != nil {
		return err
	}
	defer ctx.Close()

	if !uninstallIgnoreDeps {
		for _, name := range args {
			if !ctx.Cellar.IsInstalled(name) {
				continue
			}
			dependents, err := installedDependents(ctx, name, args)
			if err != nil {
				return err
			}
			if len(dependents) > 0 {
				ver, _ := ctx.Cellar.InstalledVersion(name)
				kegPath, _ := ctx.Cellar.KegPath(name, ver)
				return fmt.Errorf(
					"Refusing to uninstall %s\nbecause it is required by %s, which is currently installed.\nYou can override this and force removal with:\n  grew uninstall --ignore-dependencies %s",
					kegPath, strings.Join(dependents, ", "), name,
				)
			}
		}
	}

	for _, name := range args {
		if err := ctx.UninstallFormula(name, uninstallForce); err != nil {
			return err
		}
	}

	return nil
}

// installedDependents returns the names of installed packages that directly
// depend on target, excluding any packages that are also being removed in the
// same invocation (alsoRemoving).
func installedDependents(ctx *context.InstallContext, target string, alsoRemoving []string) ([]string, error) {
	removing := make(map[string]bool, len(alsoRemoving))
	for _, n := range alsoRemoving {
		removing[n] = true
	}

	packages, err := ctx.Cellar.List()
	if err != nil {
		return nil, err
	}

	var dependents []string
	for _, p := range packages {
		if p.Name == target || removing[p.Name] {
			continue
		}
		f, err := ctx.Loader.LoadByName(p.Name)
		if err != nil {
			continue
		}
		for _, dep := range f.Dependencies {
			if dep == target {
				dependents = append(dependents, p.Name)
				break
			}
		}
	}
	sort.Strings(dependents)
	return dependents, nil
}
