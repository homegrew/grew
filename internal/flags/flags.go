// Package flags defines global CLI flags shared across all grew commands.
//
// Global flags (-v/--verbose, -d/--debug, -q/--quiet) are recognised in two places:
//
//  1. Before the subcommand: [Parse] strips them from the arg list so the
//     dispatcher never sees them (e.g. "grew -v install jq").
//  2. After the subcommand: [Register] adds them to a subcommand's
//     [flag.FlagSet] so the standard flag parser accepts them
//     (e.g. "grew install -v jq").
//
// After either parse path, call [Resolve] to apply implications
// and configure the slog default logger.
package flags

import (
	"flag"

	"github.com/homegrew/grew/pkg/logger"
)

// Quiet controls whether all non-error logs are suppressed (set by -q/--quiet).
var Quiet bool

// Verbose controls whether extra detail is printed (set by -v/--verbose).
var Verbose bool

// Debug controls whether debug-level diagnostics are printed (set by -d/--debug).
// Enabling debug implicitly enables verbose.
var Debug bool

// Parse strips recognised global flags from args and returns the remaining
// arguments. It must be called before subcommand dispatch.
func Parse(args []string) []string {
	var filtered []string
	for _, a := range args {
		switch a {
		case "-q", "--quiet":
			Quiet = true
		case "-v", "--verbose":
			Verbose = true
		case "-d", "--debug":
			Debug = true
		default:
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// Register adds the global -v/--verbose, -d/--debug, and -q/--quiet flags to fs,
// bound to the package-level variables. Call this
// on every subcommand FlagSet before fs.Parse so the flags are accepted
// regardless of position on the command line.
func Register(fs *flag.FlagSet) {
	fs.BoolVar(&Quiet, "quiet", Quiet, "Only print errors")
	fs.BoolVar(&Quiet, "q", Quiet, "Only print errors")
	fs.BoolVar(&Verbose, "verbose", Verbose, "Show detailed output")
	fs.BoolVar(&Verbose, "v", Verbose, "Show detailed output")
	fs.BoolVar(&Debug, "debug", Debug, "Show debug diagnostics (implies --verbose)")
	fs.BoolVar(&Debug, "d", Debug, "Show debug diagnostics (implies --verbose)")
}

// Resolve applies flag implications and configures the
// slog default logger. Call this after [Parse] and after every subcommand
// fs.Parse that may have set the global flags.
func Resolve() {
	if Quiet {
		Verbose = false
		Debug = false
	} else if Debug {
		Verbose = true
	}
	logger.Init(Verbose, Debug, Quiet)
}
