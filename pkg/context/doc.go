// Package context provides a unified environment for grew commands. It bundles
// together essential components such as path configuration, formula and cask
// loaders, and access to the cellar and caskroom.
//
// The package simplifies command implementation by providing a shared state
// that can be passed around, ensuring that all parts of a command have access
// to the same configuration and resources.
package context
