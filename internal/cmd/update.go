package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/tap"
)

func runUpdate(args []string) error {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	count, err := tapMgr.Update()
	if err != nil {
		return fmt.Errorf("update core tap: %w", err)
	}

	fmt.Printf("==> Updated core tap (%d formulas)\n", count)
	Logf("    Tap directory: %s\n", paths.CoreTap)
	return nil
}
