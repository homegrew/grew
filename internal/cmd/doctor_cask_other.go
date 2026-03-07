//go:build !darwin

package cmd

// applyCaskQuarantine is a no-op on non-macOS platforms.
func applyCaskQuarantine(_ string) {}
