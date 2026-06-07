// Package resolver validates a dependency graph and produces an ordered
// install plan from a *depgraph.Graph.
//
// Build-only dependencies are placed after runtime dependencies in the
// resolved order. Missing nodes and dependency cycles are reported as
// typed errors (MissingError, CycleError).
package resolver
