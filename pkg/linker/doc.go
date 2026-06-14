// Package linker manages the symlinks that expose an installed keg through the
// grew prefix.
//
// A keg lives in isolation under Cellar/<name>/<version>. To make its programs
// and libraries usable, [Linker] populates the shared prefix directories —
// bin/, lib/, include/, and share/ — with symlinks pointing back into the keg,
// plus a per-formula opt/<name> link that pins a stable path to the active
// version. [Linker.Link] performs this for a keg, and [Linker.Unlink] removes
// exactly the links that resolve into a given formula's cellar subtree, leaving
// other formulas' links untouched. [Linker.IsLinked] reports whether a formula
// currently owns its opt link.
//
// # Conflicts and ownership
//
// Two formulas may ship a file of the same name (e.g. bin/node). The linker
// only replaces an existing symlink when it already belongs to the formula
// being linked — ownership is decided by resolving the link and checking it
// points inside Cellar/<name>/ — and otherwise refuses unless [LinkOpts].Overwrite
// is set. Shared directories such as lib/pkgconfig are expanded from a single
// symlink into a real directory so multiple formulas can contribute entries
// side by side.
//
// # Keg-only and version families
//
// A keg-only formula (set via [LinkOpts].KegOnly) receives only its opt link
// and is never linked into bin/lib/include/share, mirroring Homebrew. As a
// defense-in-depth backstop the linker also refuses to link a formula whose
// version family is already linked — for example node@24 when an unversioned
// node already owns bin/ links — so two members of the same family can never
// both win the shared binaries. [LinkOpts].Overwrite or .Force overrides this.
//
// # Path safety
//
// Every destination path is built from a trusted prefix root through
// pkg/safepath (joined and confined in one step) and guarded against escaping
// that root before any filesystem call, and keg targets are verified to resolve
// inside the cellar. Combined with name and version validation via
// pkg/validation, a crafted formula name or symlink cannot direct the linker to
// operate outside the grew prefix.
package linker
