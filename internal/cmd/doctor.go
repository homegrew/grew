package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"time"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
	"github.com/homegrew/grew/pkg/doctor"
)

func runDoctor(args []string) error {
	slog.Debug("starting doctor command execution")
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew doctor [options]

Check your system for potential problems.
Aliases: dr

Options:
  --list-checks List all available diagnostic checks.
  -D, --audit-debug
                Show execution time for each diagnostic check.
  -v, --verbose Show detailed output.
  -d, --debug   Show debug diagnostics (implies --verbose).
  -q, --quiet   Only print errors and warnings; omit successful checks.
`)
	}

	listChecks := fs.Bool("list-checks", false, "List all available check name")
	auditDebug := fs.Bool("audit-debug", false, "Show timing per check")
	fs.BoolVar(auditDebug, "D", false, "Show timing per check")
	runAll := fs.Bool("all", false, "Run all checks")
	fs.BoolVar(runAll, "a", false, "Run all checks")
	flags.Register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	selectedChecks := fs.Args()
	checks := append(doctor.BaseChecks(), doctor.ExtraChecks...)

	if *listChecks {
		for _, c := range checks {
			fmt.Printf("%-35s %s\n", c.Name, c.Desc)
		}
		return nil
	}

	// --all overrides any individually selected checks.
	if *runAll {
		selectedChecks = nil
	}

	// Filter to selected checks if any specified.
	if len(selectedChecks) > 0 {
		byName := make(map[string]doctor.Check, len(checks))
		for _, c := range checks {
			byName[c.Name] = c
		}
		var filtered []doctor.Check
		for _, name := range selectedChecks {
			c, ok := byName[name]
			if !ok {
				return fmt.Errorf("unknown check: %s\nRun 'grew doctor --list-checks' to see available checks", name)
			}
			filtered = append(filtered, c)
		}
		checks = filtered
	}

	paths := config.Default()

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil && !flags.Quiet {
		slog.Warn(fmt.Sprintf("failed to init core tap: %v", err))
	}

	loader := newLoader(paths.Taps)
	formulas, _ := loader.LoadAll()

	caskLoader := newCaskLoader(paths.Taps)
	casks, _ := caskLoader.LoadAll()

	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}
	packages, _ := cel.List()

	ctx := &doctor.Context{
		Paths:    paths,
		Cel:      cel,
		Lnk:      lnk,
		Loader:   loader,
		Formulas: formulas,
		Casks:    casks,
		Packages: packages,
		Quiet:    flags.Quiet,
	}
	ctx.Warn = func(format string, args ...any) {
		ctx.Warnings++
		fmt.Printf("Warning: "+format+"\n", args...)
	}

	if !flags.Quiet {
		fmt.Println("Checking grew installation...")
	}

	for _, c := range checks {
		if *auditDebug {
			start := time.Now()
			c.Run(ctx)
			if !flags.Quiet {
				fmt.Printf("[audit] %-35s %s (%d warning(s))\n", c.Name, time.Since(start), ctx.Warnings)
			}
		} else {
			c.Run(ctx)
		}
	}

	if ctx.Warnings == 0 {
		if !flags.Quiet {
			fmt.Println("Your system is ready to brew.")
		}
		return nil
	}

	if !flags.Quiet {
		fmt.Printf("\n%d warning(s) found.\n", ctx.Warnings)
	}
	// Return error so exit code is non-zero when warnings exist.
	return fmt.Errorf("%d problem(s) detected", ctx.Warnings)
}
