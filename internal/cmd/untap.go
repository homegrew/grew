package cmd

import (
	"github.com/homegrew/grew/internal/tap"
	"github.com/spf13/cobra"
)

var untapCmd = &cobra.Command{
	Use:   "untap <user/repo>",
	Short: "Remove a tapped formula repository",
	Long:  `Remove a previously tapped formula repository.`,
	Example: `  grew untap user/repo`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := newReadContext()
		if err != nil {
			return err
		}

		mgr := &tap.Manager{TapsDir: ctx.Paths.Taps}
		return mgr.Remove(args[0])
	},
}

func init() {
	rootCmd.AddCommand(untapCmd)
}
