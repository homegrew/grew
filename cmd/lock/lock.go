package lock

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/lockfile"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "lock [subcommand]",
	Short: "Manage the formula lockfile",
	Long: `Manage the formula lockfile. The lockfile records the exact state of all
installed formulas (versions, checksums, dependencies) so environments
are reproducible. It is stored at <grew_root>/var/homegrew/locks/grew.lock as JSON.

Subcommands:
  generate    Generate a lockfile from the current installed state (default)
  check       Compare the lockfile against installed packages and report
              discrepancies. Exits non-zero if any are found.
  show        Pretty-print the current lockfile`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			c, err := context.New()
			if err != nil {
				return err
			}

			return lockGenerate(c, cmd)
		}
		return fmt.Errorf("unknown lock subcommand: %s\nUsage: grew lock [generate|check|show]", args[0])
	},
}

var LockGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a lockfile from the current installed state",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := context.New()
		if err != nil {
			return err
		}
		return lockGenerate(c, cmd)
	},
}

var LockCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Compare the lockfile against installed packages and report discrepancies",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := context.New()
		if err != nil {
			return err
		}
		return lockCheck(c, cmd)
	},
}

var LockShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Pretty-print the current lockfile",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := context.New()
		if err != nil {
			return err
		}
		return lockShow(c, cmd)
	},
}

func init() {
	Command.AddCommand(LockGenerateCmd, LockCheckCmd, LockShowCmd)
}

func lockGenerate(ctx *context.Context, cmd *cobra.Command) error {
	slog.Debug("starting lock command execution")

	lf, err := lockfile.Generate(ctx)
	if err != nil {
		return fmt.Errorf("generate lockfile: %w", err)
	}

	if err := lockfile.Save(ctx, lf); err != nil {
		return fmt.Errorf("save lockfile: %w", err)
	}

	ui.Green(fmt.Sprintf("Lockfile written to %s (%d entries)\n", lockfile.LockFilePath(ctx), len(lf.Entries)), cmd.ErrOrStderr())
	return nil
}

func lockCheck(ctx *context.Context, cmd *cobra.Command) error {
	lf, err := lockfile.Load(ctx)
	if err != nil {
		return fmt.Errorf("load lockfile: %w", err)
	}

	if len(lf.Entries) == 0 {
		return fmt.Errorf("no lockfile found; run 'grew lock generate' first")
	}

	discs, err := lockfile.Check(ctx, lf)
	if err != nil {
		return fmt.Errorf("check lockfile: %w", err)
	}

	if len(discs) == 0 {
		ui.Green("Lockfile is in sync with installed packages.", cmd.ErrOrStderr())
		return nil
	}

	for _, d := range discs {
		ui.Bold(fmt.Sprintf(" %-20s %-18s %s\n", d.Name, d.Kind, d.Detail), cmd.ErrOrStderr())
	}
	return fmt.Errorf("%d discrepancies found", len(discs))
}

func lockShow(ctx *context.Context, cmd *cobra.Command) error {
	lf, err := lockfile.Load(ctx)
	if err != nil {
		return fmt.Errorf("load lockfile: %w", err)
	}

	if len(lf.Entries) == 0 {
		ui.Yellow("No lockfile found or lockfile is empty.", cmd.ErrOrStderr())
		return nil
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(lf)
}
