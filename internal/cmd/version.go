package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of grew",
	Long:  `All software has versions. This is grew's.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s\n", version.Version())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
