package update

import (
	"github.com/homegrew/grew/internal/cmd"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
    Use:     "update",
    Aliases: []string{"up"},
    Short:   "fetch the newest version of grew and all formulae",
    Long: `Fetch the newest version of grew and all formulae from GitHub
using git(1). Equivalent to: git -C <taps-dir> pull

The taps repository is cloned from:
  https://github.com/homegrew/homegrew-taps`,
    RunE: func(c *cobra.Command, args []string) error {
        return cmd.RunUpdate(args)
    },
}
