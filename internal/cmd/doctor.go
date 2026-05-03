package cmd

import (
	"fmt"
	"os"
	"github.com/homegrew/grew/pkg/ui"
	"log/slog"
	"time"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/context"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
	"github.com/homegrew/grew/pkg/doctor"
	"github.com/spf13/cobra"
)

var (
	doctorListChecks bool
	doctorAuditDebug bool
	doctorRunAll     bool
)

var DoctorCmd = &cobra.Command{
	Use:     "doctor [flags] [check ...]",
	Aliases: []string{"dr"},
	Short:   "Check your system for potential problems",
	Long: `Check your system for potential problems. Exits with non-zero status
if warnings are found.

Security checks:
  check_directory_permissions   World-writable grew directories
  check_formula_https           Formula URLs not using HTTPS
  check_formula_sha256          Invalid or malformed SHA256 hashes
  check_symlink_targets         Symlinks escaping the grew prefix
  check_cellar_permissions      World-writable installed kegs/binaries
  check_snapshot_integrity      Verify packages against install manifests

Structural checks:
  check_directories             Required directories exist
  check_path                    grew bin/ in PATH
  check_core_tap                Core tap has formulas
  check_broken_symlinks         Broken symlinks in bin/, lib/, include/
  check_broken_opt_symlinks     Broken opt/ symlinks
  check_unlinked_kegs           Installed but not linked formulas
  check_orphaned_symlinks       Symlinks to uninstalled formulas
  check_multiple_versions       Multiple versions (suggest cleanup)
  check_stale_tmp               Leftover files in tmp/

Run specific checks by name:
  grew doctor check_formula_https check_directory_permissions

Examples:
  grew doctor
  grew doctor --list-checks
  grew doctor -D
  grew doctor -q
  grew doctor check_symlink_targets`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor(args)
	},
}

func init() {
	DoctorCmd.Flags().BoolVar(&doctorListChecks, "list-checks", false, "List all available check names")
	DoctorCmd.Flags().BoolVarP(&doctorAuditDebug, "audit-debug", "D", false, "Show timing and warning count per check")
	DoctorCmd.Flags().BoolVarP(&doctorRunAll, "all", "a", false, "Run all checks (overrides individual selections)")
	rootCmd.AddCommand(DoctorCmd)
}

func runDoctor(args []string) error {
	slog.Debug("starting doctor command execution")

	selectedChecks := args
	checks := append(doctor.BaseChecks(), doctor.ExtraChecks...)

	if doctorListChecks {
		for _, c := range checks {
			fmt.Printf("%-35s %s\n", c.Name, c.Desc)
		}
		return nil
	}

	// --all overrides any individually selected checks.
	if doctorRunAll {
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

	loader := context.NewLoader(paths.Taps)
	formulas, _ := loader.LoadAll()

	caskLoader := context.NewCaskLoader(paths.Taps)
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
		fmt.Fprintf(os.Stdout, "%s "+format+"\n", append([]any{ui.ArrowWarning(os.Stdout)}, args...)...)
	}

	if !flags.Quiet {
		fmt.Println("Checking grew installation...")
	}

	for _, c := range checks {
		if doctorAuditDebug {
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
