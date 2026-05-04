// Package lock implements the 'lock' command.
//
// Manage the formula lockfile
//
// Manage the formula lockfile. The lockfile records the exact state of all
// installed formulas (versions, checksums, dependencies) so environments
// are reproducible. It is stored at <grew_root>/grew.lock as JSON.
package lock
