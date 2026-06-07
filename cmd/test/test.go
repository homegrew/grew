package test

import (
	"context"
	"fmt"
	"os"

	grewctx "github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/hooks"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "test <formula>",
	Short: "Run a formula's test hook in isolation",
	Long: `Run a formula's test hook without installing or modifying any packages.

The TestHook identifier declared in the formula is used to drive the
pre-test and post-test lifecycle phases. Runtime dependencies are not
installed or re-resolved.

Examples:
  grew test jq
  grew test openssl`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return RunTest(args[0])
	},
}

// RunTest executes the test lifecycle for the named formula.
func RunTest(name string) error {
	appCtx, err := grewctx.New()
	if err != nil {
		return fmt.Errorf("init context: %w", err)
	}

	f, err := appCtx.LoadFormula(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	if f.TestHook == "" {
		ui.FprintArrow(os.Stderr, "No test hook defined for %s", f.Name)
		return nil
	}

	ui.FprintArrow(os.Stderr, "Testing %s %s (test hook: %s)", f.Name, f.Version, f.TestHook)

	hookSet := &hooks.HookSet{
		PreTest:  []hooks.Hook{hooks.NewNoopHook("pre-test:"+f.TestHook, nil)},
		PostTest: []hooks.Hook{hooks.NewNoopHook("post-test:"+f.TestHook, nil)},
	}

	env := hooks.Env{
		Prefix:  appCtx.Paths.Root,
		Cellar:  appCtx.Paths.Cellar,
		Formula: f.Name,
		Version: f.Version,
		Tmpdir:  appCtx.Paths.Tmp,
	}

	ctx := context.Background()
	if err := hookSet.RunPhase(ctx, hooks.PhasePreTest, env); err != nil {
		return fmt.Errorf("pre-test for %s: %w", f.Name, err)
	}
	if err := hookSet.RunPhase(ctx, hooks.PhasePostTest, env); err != nil {
		return fmt.Errorf("post-test for %s: %w", f.Name, err)
	}

	ui.FprintArrow(os.Stderr, "%s: OK", f.Name)
	return nil
}
