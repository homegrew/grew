package ui

import (
	"fmt"
	"io"
	"os"
)

// ANSI escape codes
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	blue   = "\033[34m"
	cyan   = "\033[36m"
)

// Config holds global UI rendering options.
type Config struct {
	// NoColor disables ANSI colour codes regardless of whether the output
	// stream is a terminal. Set this to honour the NO_COLOR convention or
	// when piping output to a file.
	NoColor bool
}

// DefaultConfig is the package-level UI configuration. Commands may mutate
// this (e.g. in response to --no-color) before producing any output.
var DefaultConfig = Config{}

// isTerminal checks if the given file descriptor is a terminal.
func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func colorize(colorCode, text string, out io.Writer) string {
	if DefaultConfig.NoColor {
		return text
	}
	
	// Fast path for os.Stdout / os.Stderr check
	if f, ok := out.(*os.File); ok {
		if !isTerminal(f) {
			return text
		}
	}
	
	return colorCode + text + reset
}

// Bold returns text wrapped in the bold ANSI escape sequence when out is a
// terminal and colour is not disabled.
func Bold(text string, out io.Writer) string {
	return colorize(bold, text, out)
}

// Red returns text in red when out is a terminal and colour is not disabled.
func Red(text string, out io.Writer) string {
	return colorize(red, text, out)
}

// Green returns text in green when out is a terminal and colour is not disabled.
func Green(text string, out io.Writer) string {
	return colorize(green, text, out)
}

// Yellow returns text in yellow when out is a terminal and colour is not disabled.
func Yellow(text string, out io.Writer) string {
	return colorize(yellow, text, out)
}

// Blue returns text in blue when out is a terminal and colour is not disabled.
func Blue(text string, out io.Writer) string {
	return colorize(blue, text, out)
}

// Cyan returns text in cyan when out is a terminal and colour is not disabled.
func Cyan(text string, out io.Writer) string {
	return colorize(cyan, text, out)
}

// Arrow is a pre-formatted, Homebrew-style output prefix (==>)
func Arrow(out io.Writer) string {
	return colorize(blue+bold, "==>", out)
}

// ArrowError is a pre-formatted, error output prefix (Error:)
func ArrowError(out io.Writer) string {
	return colorize(red+bold, "Error:", out)
}

// ArrowWarning is a pre-formatted warning prefix (Warning:)
func ArrowWarning(out io.Writer) string {
	return colorize(yellow+bold, "Warning:", out)
}

// FprintArrow is a convenience method for writing the "==>" prefix and a message
func FprintArrow(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "%s %s\n", Arrow(out), fmt.Sprintf(format, args...))
}

// FprintError is a convenience method for writing the "Error:" prefix and a message
func FprintError(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "%s %s\n", ArrowError(out), fmt.Sprintf(format, args...))
}
