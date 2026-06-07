// Package safepath provides path-safety primitives that prevent directory
// traversal, Zip Slip, and argument-injection attacks throughout grew.
//
// Every filesystem operation that touches an externally influenced path —
// archive entries, tap contents, asset names, cellar keg paths — must pass
// through this package before reaching os.Open, os.Rename, or
// filepath.Join. The functions here are the single authoritative layer for
// path validation; duplicating the logic elsewhere is explicitly avoided so
// that security audits have one place to check.
//
// # Core primitives
//
//   - [SafeJoin] — joins path components onto a base and errors if the result
//     escapes the base. This is the preferred primitive: it validates at the
//     join site rather than after the fact, which is also the form recognised
//     by static taint-analysis tools.
//
//   - [CheckSubpath] / [IsSubpath] — assert that an already-constructed path
//     stays within a given base. Use when the path is built elsewhere (e.g.
//     before deleting a keg directory).
//
//   - [CleanPath] — rejects ".." traversal markers and returns the cleaned
//     path. Use to validate a directory path before appending a trusted name.
//
//   - [SafePathComponent] — validates a single filename segment: no
//     separators, null bytes, or ".." markers. Use for any user-supplied
//     name before it reaches filepath.Join.
//
//   - [SafeAbsolutePath] — requires the path to be absolute, clean, and not
//     the filesystem root "/". Used to validate prefix and keg paths at
//     context initialisation time.
//
//   - [NormalizeDir] — resolves symlinks and calls SafeAbsolutePath;
//     convenient for validating directory arguments from config or flags.
//
//   - [URLExt] — extracts the file extension from a URL, handling compound
//     extensions like ".tar.gz".
package safepath
