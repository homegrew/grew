// Package doctor provides a diagnostic engine for verifying the health, security,
// and structural integrity of a grew installation.
//
// The diagnostic process is built around two core concepts:
//
//   - Context: A container for shared state used across diagnostic checks. It
//     provides access to the cellar, linker, formula loaders, and current
//     installation state (formulas, casks, and installed packages).
//   - Check: A named diagnostic operation that performs a specific verification
//     against the provided Context and reports warnings if issues are detected.
//
// # Usage
//
// Diagnostics are typically run by initializing a Context and then executing
// a series of Checks. The package provides BaseChecks() for core cross-platform
// verifications and ExtraChecks for platform-specific diagnostics (e.g., macOS
// cask security).
//
// Example:
//
//	ctx := &doctor.Context{
//	    Paths: config.Default(),
//	    Warn:  func(f string, a ...any) { fmt.Printf(f, a...) },
//	}
//	// ... populate ctx with cellar, linker, etc.
//
//	for _, check := range doctor.BaseChecks() {
//	    check.Run(ctx)
//	}
//
// # Extensibility
//
// Platform-specific checks (like Darwin cask verifications) register themselves
// via init() using RegisterExtraChecks. This allows the diagnostic suite to
// automatically adapt to the host environment.
package doctor
