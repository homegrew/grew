package pin

import (
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "pin <formula>...",
	Short: "Pin formulas to prevent upgrades",
	Long:  "Pin formulas to prevent them from being upgraded automatically.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPin(args)
	},
}

func runPin(args []string) error {
	slog.Debug("starting pin command execution")
	
	if len(args) == 0 {
		return fmt.Errorf("usage: grew pin <formula>...")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}
	logger := auditlog.New(paths.Log)

	for _, name := range args {
		if !cel.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		if cel.IsPinned(name) {
			fmt.Printf("%s is already pinned.\n", name)
			continue
		}

		if err := cel.Pin(name); err != nil {
			return err
		}
		logger.Log(auditlog.ActionPin, name, "", "", "")
		fmt.Printf("Pinned %s (will not be upgraded).\n", name)
	}

	return nil
}
