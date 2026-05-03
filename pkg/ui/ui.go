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

// Config represents global UI configuration
type Config struct {
	NoColor bool
}

// Global configuration
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

// Formatters
func Bold(text string, out io.Writer) string {
	return colorize(bold, text, out)
}

func Red(text string, out io.Writer) string {
	return colorize(red, text, out)
}

func Green(text string, out io.Writer) string {
	return colorize(green, text, out)
}

func Yellow(text string, out io.Writer) string {
	return colorize(yellow, text, out)
}

func Blue(text string, out io.Writer) string {
	return colorize(blue, text, out)
}

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
