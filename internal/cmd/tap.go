package cmd

import (
	"fmt"
	"strings"

	"github.com/homegrew/grew/internal/tap"
)

func runTap(args []string) error {
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
}
