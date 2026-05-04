// Package cleanup implements the 'cleanup' command.
//
// Remove old versions and temp files
//
// Remove old versions of installed formulas and clear old downloads from the cache.
// By default, it keeps the latest version of each installed formula and its
// associated download, but removes downloads older than 120 days.
package cleanup
