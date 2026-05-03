package cmd

import (
	"fmt"

	"github.com/homegrew/grew/internal/tap"
)

func runUntap(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("untap requires a tap name (e.g. user/repo)")
	}

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	mgr := &tap.Manager{TapsDir: ctx.Paths.Taps}
	return mgr.Remove(args[0])
}
