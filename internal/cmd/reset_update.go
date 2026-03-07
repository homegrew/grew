package cmd

import (
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/tap"
)

func runResetUpdate(args []string) error {
	if len(args) > 0 {
		if args[0] == "--help" || args[0] == "-h" {
			return runHelp([]string{"reset-update"})
		}
		return fmt.Errorf("unknown flag: %s\nRun 'grew help reset-update' for usage", args[0])
	}

	paths := config.Default()

	fmt.Printf("==> Removing taps directory %s\n", paths.Taps)
	if err := os.RemoveAll(paths.Taps); err != nil {
		return fmt.Errorf("remove taps: %w", err)
	}

	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	count, err := tapMgr.Update()
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	fmt.Printf("==> Tap definitions reset and updated (%d formulas)\n", count)
	return nil
}
