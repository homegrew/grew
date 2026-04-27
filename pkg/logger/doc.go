// Package logger configures the default [log/slog] logger for CLI use.
//
// The package provides a custom [slog.Handler] that formats log records
// in a style suitable for terminal output:
//
//   - DEBUG messages are prefixed with "sourceFile:line: " (e.g., "downloader.go:42: ")
//   - INFO  messages are indented with four spaces
//   - WARN  messages are prefixed with "Warning: "
//   - ERROR messages are prefixed with "Error: "
//
// Call [Init] early in the program (after flag parsing) to set the global
// default logger. Without Init the default slog logger is used as-is.
package logger
