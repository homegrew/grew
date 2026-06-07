package version

import (
	"fmt"

	intver "github.com/homegrew/grew/pkg/version"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of grew",
	Long:  `All software has versions. This is grew's.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s\n", intver.Version())
	},
}

func init() {
}
