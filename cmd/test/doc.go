// Package test implements the 'test' command.
//
// Run a formula's test hook in isolation
//
// Loads the named formula, constructs a HookSet from the formula's TestHook
// field, and executes the pre-test and post-test lifecycle phases. Runtime
// dependencies are not installed; only the test hook is run.
package test
