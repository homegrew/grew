package cmd

import (
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
)

func runPin(args []string) error {
	slog.Debug("starting pin command execution")
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

func runUnpin(args []string) error {
	slog.Debug("starting unpin command execution")
	slog.Debug("starting unpin command execution")
	if len(args) == 0 {
		return fmt.Errorf("usage: grew unpin <formula>...")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}
	logger := auditlog.New(paths.Log)

	for _, name := range args {
		if !cel.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		if !cel.IsPinned(name) {
			fmt.Printf("%s is not pinned.\n", name)
			continue
		}

		if err := cel.Unpin(name); err != nil {
			return err
		}
		logger.Log(auditlog.ActionUnpin, name, "", "", "")
		fmt.Printf("Unpinned %s.\n", name)
	}

	return nil
}
