// Package quarantine manages macOS quarantine extended attributes and Trash
// operations for downloaded applications and binaries.
//
// # Quarantine attributes
//
// macOS Gatekeeper relies on the com.apple.quarantine extended attribute to
// trigger security checks the first time a downloaded executable or app bundle
// is launched. grew applies this attribute via [Apply] after every download so
// that Gatekeeper protection is active even for packages installed outside
// Safari or a browser.
//
// [Apply] invokes an embedded Swift script (quarantine.swift) via
// /usr/bin/swift. The script calls the LaunchServices API, which correctly
// registers the download origin and URL — the same metadata a browser would
// write. Using LaunchServices rather than xattr(1) ensures the attribute
// format matches what Gatekeeper expects.
//
// # Trash
//
// [Trash] moves one or more paths to the user's Trash via an embedded Swift
// script (trash.swift), again using Finder/LaunchServices semantics. This
// preserves the original file name in the Trash and allows easy recovery,
// unlike rm(1). grew uses this when uninstalling cask applications.
//
// # Script execution
//
// [RunScript] is the shared entry point: it writes the embedded script bytes
// to a temporary directory and invokes /usr/bin/swift, capturing stdout and
// stderr separately. Embedded scripts are compiled by the Swift interpreter at
// run time; no pre-compilation is required.
//
// All three embedded scripts (quarantine.swift, trash.swift,
// copy-xattrs.swift) are compiled into the grew binary via go:embed.
package quarantine
