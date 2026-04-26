package cmd

import (
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/runtime"
	"github.com/homegrew/grew/internal/tap"
)

func runUpdate(args []string) error {
	// Attempt to self-update the CLI binary first, unless we are in devmode.
	if !runtime.DevMode {
		fmt.Println("==> Checking for grew updates...")
		if err := RunSelfUpdate(nil); err != nil {
			slog.Warn("self-update failed, continuing with tap update", "error", err)
		}
	} else {
		slog.Info("skipping self-update in devmode build")
	}

	// Update tap definitions.
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
	slog.Info("tap directory: " + paths.CoreTap)
	return nil
}
