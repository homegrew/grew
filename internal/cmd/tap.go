package cmd

import (
	"fmt"
	"strings"

	"github.com/homegrew/grew/internal/tap"
	"github.com/spf13/cobra"
)

var tapCmd = &cobra.Command{
	Use:   "tap [user/repo] [url]",
	Short: "Tap a formula repository",
	Long: `Tap a formula repository.
With no arguments, lists currently installed taps.
With arguments, taps the specified repository.`,
	Example: `  grew tap
  grew tap user/repo
  grew tap user/repo https://github.com/user/repo`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := newReadContext()
		if err != nil {
			return err
		}

		mgr := &tap.Manager{TapsDir: ctx.Paths.Taps}

		if len(args) == 0 {
			taps, err := mgr.List()
			if err != nil {
				return err
			}
			for _, t := range taps {
				fmt.Println(t)
			}
			return nil
		}

		// Support 'grew tap user/repo [url]'
		name := args[0]
		customURL := ""
		if len(args) > 1 {
			customURL = args[1]
		}

		if !strings.Contains(name, "/") {
			return fmt.Errorf("invalid tap name %q; expected \"user/repo\"", name)
		}

		return mgr.Add(name, customURL)
	},
}

func init() {
	rootCmd.AddCommand(tapCmd)
}
