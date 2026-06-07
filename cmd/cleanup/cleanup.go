package cleanup

import (
	"github.com/homegrew/grew/pkg/cmd"
	"github.com/spf13/cobra"
)

var (
	cleanupDryRun  bool
	cleanupScrub   bool
	cleanupPrune   string
)

var Command = &cobra.Command{
	Use:   "cleanup [flags] [formula ...]",
	Short: "Remove old versions and temp files",
	Long: `Remove old versions of installed formulas and clear old downloads from the cache.
By default, it keeps the latest version of each installed formula and its 
associated download, but removes downloads older than 120 days.

Examples:
  grew cleanup
  grew cleanup -n
  grew cleanup --scrub
  grew cleanup --prune=7
  grew cleanup jq`,
	RunE: func(c *cobra.Command, args []string) error {
		opts := cmd.CleanupOpts{
			DryRun: cleanupDryRun,
			Scrub:  cleanupScrub,
			Prune:  cleanupPrune,
		}
		return cmd.RunCleanup(args, opts)
	},
}

func init() {
	Command.Flags().BoolVarP(&cleanupDryRun, "dry-run", "n", false, "Show what would be removed, but do not actually remove anything.")
	Command.Flags().BoolVarP(&cleanupScrub, "scrub", "s", false, "Remove all cached downloads, including those for the latest versions.")
	Command.Flags().StringVar(&cleanupPrune, "prune", "", "Remove all cache files older than specified days (or \"all\").")
}
