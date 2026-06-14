// Package formula defines grew's formula data model and the loader that
// discovers formula definitions inside tapped repositories.
//
// A [Formula] is the parsed YAML description of a CLI package: its name,
// version, metadata, dependency graph, and the per-platform artifacts grew
// downloads and installs. [Parse] unmarshals YAML and runs [Formula.Validate],
// which enforces grew's invariants — required fields, valid names/versions,
// HTTPS-only URLs, safe path components, and a recognized install type — so
// that malformed or unsafe definitions are rejected before any download
// occurs.
//
// # Platform resolution
//
// Formulas describe artifacts for multiple platforms, keyed by a platform
// string such as darwin_arm64_15 (OS, architecture, and — on the current
// macOS host — the major OS version). [GetPlatformKey] computes that key, and
// the GetURL/GetSHA256/GetSHA512/GetSignature accessors select the best match
// for a platform, preferring a version-specific key and falling back to the
// generic OS_arch key.
//
// Two artifact schemas are supported. The current schema nests per-platform
// downloads under [BottleSpec] (the Bottle map); the legacy schema uses the
// top-level URL/SHA256/SHA512/Signature maps. The accessors transparently
// resolve either, always reading a bottle's URL and both hashes from the same
// key so values cannot be mixed across versions. [Formula.ResolveForceBottle]
// backs `grew install --force-bottle`, selecting the current-version bottle or,
// failing that, the newest available macOS-version bottle for the host
// architecture — never falling back to a source build.
//
// # Loading
//
// [Loader] resolves formula names against the tap tree rooted at TapDir.
// [Loader.LoadByName] accepts a bare name (searched across every tapped
// user/repo) or a tap-qualified name (user/repo/name, with core/ and cask/
// redirected to the bundled homegrew tap), probing the standard layout
// variants (repo root, core/, Formula/, and first-letter subdirectories).
// [Loader.LoadAll] and [Loader.LoadFromTap] enumerate whole taps, and
// [Loader.GatherDeps] walks the dependency graph transitively.
//
// Every path the loader touches is validated through pkg/safepath and confined
// to TapDir, so a crafted name or symlinked tap cannot escape the taps
// directory.
package formula
