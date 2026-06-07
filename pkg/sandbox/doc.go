// Package sandbox wraps external commands in platform-appropriate isolation
// to prevent formula builds, post-install scripts, and archive extraction
// from reaching the network or writing outside their designated directories.
//
// # Profiles
//
// Three operation types are supported, each with its own config and
// corresponding *Command constructor:
//
//   - [Command] / [BuildConfig] — source builds. Writes allowed to the build
//     and keg directories; all file reads allowed (toolchains, SDK headers,
//     dyld shared cache). Network denied.
//
//   - [PostInstallCommand] / [PostInstallConfig] — post-install hooks. The
//     installed keg is read-only; only a temporary scratch directory is
//     writable. Network denied.
//
//   - [ExtractCommand] / [ExtractConfig] — archive extraction. Writes confined
//     to the staging directory. Network denied.
//
// # Platform behaviour
//
// On macOS, isolation is enforced by sandbox-exec(1) with a dynamically
// generated Seatbelt profile. [IsSandboxed] probes at first use (via a
// no-op sandbox-exec invocation) and caches the result; in nested sandbox
// environments where sandbox-exec is present but non-functional the probe
// fails and the package falls back to a clean-environment-only mode.
//
// On non-macOS hosts, or when Seatbelt is unavailable, commands still run
// with a scrubbed environment (no secrets, no compiler variables beyond the
// essential set) but without OS-enforced write or network restrictions.
//
// # Environment scrubbing
//
// Regardless of platform, each operation type receives only the minimum set
// of environment variables it legitimately needs:
//
//   - Build: PATH, HOME, compiler flags (CC/CXX/CFLAGS/…), SDK vars
//   - Post-install / extract: PATH, HOME, LANG — no compiler variables
//
// TMPDIR is always overridden to point inside the operation's working area
// so compiler temporaries do not escape to the real system temp directory.
package sandbox
