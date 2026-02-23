package cmd

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/version"
)

var embeddedTaps fs.FS

// Verbose controls whether extra detail is printed.
var Verbose bool

// Debug controls whether debug-level diagnostics are printed.
// Enabling debug implicitly enables verbose.
var Debug bool

// Logf prints only when Verbose (or Debug) is true.
func Logf(format string, args ...any) {
	if Verbose {
		fmt.Printf(format, args...)
	}
}

// Debugf prints only when Debug is true. Prefixed with "[debug]".
func Debugf(format string, args ...any) {
	if Debug {
		fmt.Printf("[debug] "+format, args...)
	}
}

// TimeOp logs the duration of an operation when Debug is true.
// Usage: defer TimeOp("downloading")()
func TimeOp(label string) func() {
	if !Debug {
		return func() {}
	}
	start := time.Now()
	Debugf("%s started\n", label)
	return func() {
		Debugf("%s completed in %s\n", label, time.Since(start))
	}
}

func Run(args []string, taps fs.FS) error {
	embeddedTaps = taps

	// Strip global flags before dispatching.
	var filtered []string
	for _, a := range args {
		switch a {
		case "-v", "--verbose":
			Verbose = true
		case "-d", "--debug":
			Debug = true
			Verbose = true // debug implies verbose
		case "--version":
			fmt.Printf("grew %s\n", version.Version())
			return nil
		default:
			filtered = append(filtered, a)
		}
	}
	args = filtered

	if len(args) == 0 {
		printUsage()
		return nil
	}

	// Handle "grew --help" and "grew -h"
	if args[0] == "--help" || args[0] == "-h" {
		return runHelp(args[1:])
	}

	commands := map[string]func([]string) error{
		"install":   runInstall,
		"uninstall": runUninstall,
		"remove":    runUninstall,
		"list":      runList,
		"info":      runInfo,
		"search":    runSearch,
		"link":      runLink,
		"unlink":    runUnlink,
		"update":    runUpdate,
		"upgrade":   runUpgrade,
		"outdated":  runOutdated,
		"reinstall": runReinstall,
		"cleanup":   runCleanup,
		"deps":      runDeps,
		"alias":     runAlias,
		"doctor":    runDoctor,
		"dr":        runDoctor,
		"config":    runConfig,
		"shellenv":  runShellenv,
		"help":      runHelp,
	}

	handler, ok := commands[args[0]]
	if !ok {
		// Check aliases
		a, err := loadAliases()
		if err != nil {
			Debugf("failed to load aliases: %v\n", err)
		} else if target, exists := a[args[0]]; exists {
			expanded := append([]string{target}, args[1:]...)
			if h, found := commands[expanded[0]]; found {
				return h(expanded[1:])
			}
		}
		return fmt.Errorf("unknown command: %s\nRun 'grew' for usage", args[0])
	}
	return handler(args[1:])
}

// newLoader creates a formula.Loader with debug logging wired in.
func newLoader(tapDir string) *formula.Loader {
	l := &formula.Loader{TapDir: tapDir}
	if Debug {
		l.DebugLog = func(format string, args ...any) {
			Debugf(format, args...)
		}
	}
	return l
}

func printUsage() {
	fmt.Print(`grew - a package manager written in Go

Usage:
  grew [flags] <command> [arguments]

Flags:
  -v, --verbose        Show detailed output
  -d, --debug          Show debug diagnostics (implies --verbose)
      --version        Print version and exit

Commands:
  install <formula>    Install a formula (use --cask for apps)
  uninstall <formula>  Uninstall a formula or cask (--cask)
  list                 List installed formulas or casks (--cask)
  info <formula>       Show formula or cask info (--cask)
  search <query>       Search formulas or casks (--cask)
  link <formula>       Create symlinks for a formula
  unlink <formula>     Remove symlinks for a formula
  update               Update formula definitions
  reinstall <formula>  Reinstall a formula from scratch
  upgrade [formula]    Upgrade outdated packages (or a specific one)
  outdated             List packages with newer versions available
  cleanup [-n]         Remove old versions and temp files (-n for dry run)
  deps [flags] <formula>  Show dependencies for a formula
  alias [subcommand]   Manage command aliases
  doctor               Check for common problems
  config               Show grew and system configuration
  shellenv [shell]     Print shell environment setup
  help [command]       Show help for a command
`)
}
