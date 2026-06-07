// Package ui provides the terminal output primitives used throughout grew's
// CLI — ANSI colour formatting, Homebrew-style arrow prefixes, and TTY
// detection.
//
// # Colour handling
//
// Colour is applied only when the destination writer is *os.Stdout or
// *os.Stderr and the file descriptor is a character device (i.e. a real
// terminal). When output is piped or redirected the escape codes are omitted
// automatically. Colour can also be suppressed globally by setting
// [DefaultConfig].NoColor = true, which commands do in response to a
// --no-color flag or the NO_COLOR environment variable.
//
// # Arrow prefixes
//
// grew follows Homebrew's visual convention of prefixing progress lines with
// a bold blue "==>" marker. The helpers [FprintArrow], [FprintError], and
// the prefix-only variants [Arrow], [ArrowError], [ArrowWarning] apply the
// correct colour and write to the supplied io.Writer.
//
// All functions accept an io.Writer so that callers can direct output to
// stdout, stderr, or a test buffer without changing the formatting logic.
package ui
