// Package install implements the 'install' command.
//
// Install formulas or casks.
//
// Install one or more formulas or casks, along with their dependencies. Each
// package is downloaded, verified against its SHA256 checksum, extracted to the
// Cellar, and linked into the prefix.
//
// Each argument is auto-detected as a formula or a cask, with a formula taking
// precedence when both exist. The mutually-exclusive --formula and --cask flags
// pin every argument to a single kind and disable the other. Arguments are
// processed in order, and installation stops at the first failure.
package install
