// Package cask defines grew's cask data model and the machinery that loads,
// records, and installs macOS GUI applications.
//
// A [Cask] is the parsed YAML description of a macOS application package: its
// name, version, metadata, per-platform downloads, and the [Artifacts] to
// install from the archive — .app bundles copied into ~/Applications, .pkg
// installers run via the system installer, and bare binaries symlinked into
// grew's bin/. [Parse] unmarshals YAML and runs [Cask.Validate], which enforces
// grew's invariants (required fields, valid names/versions, HTTPS-only URLs,
// well-formed hashes, and at least one declared artifact) before any download
// occurs.
//
// Unlike formulas, casks key their downloads by a plain OS_arch platform string
// (no macOS-version component); the GetURL/GetSHA256/GetSHA512 accessors select
// the entry for the current host and reject insecure HTTP.
//
// # Loading
//
// [Loader] resolves cask names against the tap tree rooted at TapDir.
// [Loader.LoadByName] accepts a bare name (searched across every tapped
// user/repo) or a tap-qualified name (user/repo/name, with core/ and cask/
// redirected to the bundled homegrew tap), probing the standard layout variants
// (repo root, cask/, Casks/, and first-letter subdirectories). [Loader.LoadAll]
// enumerates whole taps and [Loader.Search] filters by name or description.
// Every path is validated through pkg/safepath and confined to TapDir.
//
// # Installed state
//
// [Caskroom] tracks what is installed by managing Caskroom/<name>/<version>/
// directories: Record marks an install, Remove tears one down, and
// IsInstalled/InstalledVersion/List report current state. All entries are
// name- and version-validated and confined to the caskroom path.
//
// # Installation
//
// [Installer] places artifacts into their destinations. InstallApp copies a
// .app bundle (located anywhere in the staging tree) into AppDir, with symlink
// escape protection ensuring the source resolves inside the staging directory
// and trashing any existing copy before overwriting; InstallPkg runs a .pkg via
// sudo.
package cask
