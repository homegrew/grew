// Package logger configures the default [log/slog] logger for CLI use.
//
// The package provides a custom [slog.Handler] that formats log records
// in a style suitable for terminal output:
//
//   - DEBUG messages are prefixed with "[debug] "
//   - INFO  messages are indented with four spaces
//   - WARN  messages are prefixed with "Warning: "
//   - ERROR messages are prefixed with "Error: "
//
// Call [Init] early in the program (after flag parsing) to set the global
// default logger. Without Init the default slog logger is used as-is.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// Init configures the default slog logger for CLI output.
//
// Log level mapping:
//   - verbose=false, debug=false → LevelWarn  (only warnings and errors)
//   - verbose=true               → LevelInfo  (verbose detail)
//   - debug=true                 → LevelDebug (full diagnostics)
func Init(verbose, debug bool) {
	level := slog.LevelWarn
	if debug {
		level = slog.LevelDebug
	} else if verbose {
		level = slog.LevelInfo
	}
	h := &CLIHandler{
		w:     os.Stderr,
		level: level,
	}
	slog.SetDefault(slog.New(h))
}

// TimeOp logs the start and duration of an operation at DEBUG level.
// Usage:
//
//	defer logger.TimeOp("downloading")()
func TimeOp(label string) func() {
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return func() {}
	}
	start := time.Now()
	slog.Debug(label + " started")
	return func() {
		slog.Debug(fmt.Sprintf("%s completed in %s", label, time.Since(start)))
	}
}

// CLIHandler is a [slog.Handler] that writes human-friendly log lines to a
// writer (typically os.Stderr). It is safe for concurrent use.
type CLIHandler struct {
	w     io.Writer
	level slog.Level
	mu    sync.Mutex
}

func (h *CLIHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *CLIHandler) Handle(_ context.Context, r slog.Record) error {
	var prefix string
	switch {
	case r.Level >= slog.LevelError:
		prefix = "Error: "
	case r.Level >= slog.LevelWarn:
		prefix = "Warning: "
	case r.Level >= slog.LevelInfo:
		prefix = "    "
	default: // DEBUG and below
		prefix = "[debug] "
	}

	buf := make([]byte, 0, 128)
	buf = append(buf, prefix...)
	buf = append(buf, r.Message...)

	if r.NumAttrs() > 0 {
		r.Attrs(func(a slog.Attr) bool {
			buf = append(buf, ' ')
			buf = append(buf, a.Key...)
			buf = append(buf, '=')
			buf = append(buf, a.Value.String()...)
			return true
		})
	}

	buf = append(buf, '\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf)
	return err
}

func (h *CLIHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *CLIHandler) WithGroup(_ string) slog.Handler      { return h }
