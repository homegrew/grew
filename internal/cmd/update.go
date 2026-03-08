package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/tap"
)

func runUpdate(args []string) error {
	// Step 1: Update grew itself via git pull + go build.
	if err := runSelfUpdate(nil); err != nil {
		// Non-fatal: warn and continue with tap update.
		fmt.Printf("==> Warning: self-update failed: %v\n", err)
	}

	// Step 2: Update tap definitions.
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	count, err := tapMgr.Update()
	if err != nil {
		return fmt.Errorf("update core tap: %w", err)
	}

	fmt.Printf("==> Updated core tap (%d formulas)\n", count)
	Logf("    Tap directory: %s\n", paths.CoreTap)
	return nil
}
