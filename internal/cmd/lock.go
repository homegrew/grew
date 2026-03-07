package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/lockfile"
)

func runLock(args []string) error {
	sub := "generate"
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "generate":
		return lockGenerate()
	case "check":
		return lockCheck()
	case "show":
		return lockShow()
	default:
		return fmt.Errorf("unknown lock subcommand: %s\nUsage: grew lock [generate|check|show]", sub)
	}
}

func lockGenerate() error {
	paths := config.Default()

	lf, err := lockfile.Generate(paths.Root, paths.Cellar)
	if err != nil {
		return fmt.Errorf("generate lockfile: %w", err)
	}

	if err := lockfile.Save(lf, paths.Root); err != nil {
		return fmt.Errorf("save lockfile: %w", err)
	}

	fmt.Printf("Lockfile written to %s (%d entries)\n", lockfile.LockFilePath(paths.Root), len(lf.Entries))
	return nil
}

func lockCheck() error {
	paths := config.Default()

	lf, err := lockfile.Load(paths.Root)
	if err != nil {
		return fmt.Errorf("load lockfile: %w", err)
	}

	if len(lf.Entries) == 0 {
		return fmt.Errorf("no lockfile found; run 'grew lock generate' first")
	}

	discs, err := lockfile.Check(lf, paths.Cellar)
	if err != nil {
		return fmt.Errorf("check lockfile: %w", err)
	}

	if len(discs) == 0 {
		fmt.Println("Lockfile is in sync with installed packages.")
		return nil
	}

	for _, d := range discs {
		fmt.Printf("  %-20s %-18s %s\n", d.Name, d.Kind, d.Detail)
	}
	return fmt.Errorf("%d discrepancies found", len(discs))
}

func lockShow() error {
	paths := config.Default()

	lf, err := lockfile.Load(paths.Root)
	if err != nil {
		return fmt.Errorf("load lockfile: %w", err)
	}

	if len(lf.Entries) == 0 {
		fmt.Println("No lockfile found or lockfile is empty.")
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(lf)
}
