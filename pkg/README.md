# pkg — package overview

Each subdirectory is a standalone Go package. This document describes what each
one is for and what it exports. Deeper implementation details live in
[docs/tech.md](../docs/tech.md).

---

## Core layer

### `config`
Central path and configuration resolution. Locates the grew prefix (inferred
from the binary path or `HOMEGREW_PREFIX`), builds all sub-directory paths
(Cellar, Taps, Cache, etc.), and loads env-var overrides.
Key export: `Default() *Config`, `DefaultPrefix() string`.

### `context`
Unified execution environment passed to every command. Bundles `Paths`,
formula/cask loaders, and the Cellar/Caskroom managers into a single object
so commands don't read global variables.
- `Context` — read-only operations.
- `InstallContext` — destructive operations; holds a process-wide advisory lock.
- `LoadFormula` / `LoadCask` — encapsulate auto-tapping and Homebrew API fallback.

### `runtime`
Determines the effective runtime environment: prefix, whether the process is
running as root, and the devmode gate (`DevMode && --unsafe`). Exports
`DevMode bool` (set at compile time via build tag) and `devModeActive()`.

### `flags`
Registers the global `-v` / `-d` / `-q` verbosity flags that every command
inherits. Sets up the `log/slog` level before command execution begins.

---

## Security primitives

### `safepath`
The single layer for path-traversal and Zip-Slip prevention. All filesystem
operations that touch externally-influenced paths (archive entries, asset
names, tap paths, cellar kegs) are routed through here **before** the path
reaches `os.Open` or `filepath.Join`.
- `SafeJoin(base, components...)` — join + escape check at the join site.
- `CheckSubpath(base, target)` — assert an already-built path stays within base.
- `CleanPath(path)` — reject `..` traversal markers.
- `SafePathComponent(name)` — validate a single filename component.
- `SafeAbsolutePath(path)` — require absolute, clean, non-root path.

### `fsutil`
Safe file-system helpers shared across the codebase.
- `WriteFileAtomic(dst, data, mode)` — write to a temp file, chmod, close,
  then `rename` into place. Used by snapshot, receipt, and release to prevent
  partial writes or corruption on crash.
- `CopyTree` / `CopyFileWithinRoot` — directory copies with root-confinement
  checks.
- `Lock` / `TryLock` — advisory `flock`-based file locking.
- `SanitizeMode` — strips setuid/setgid/sticky/world-write bits during archive
  extraction.

### `signing`
Ed25519 bottle signing and trust-store management.
- `Sign(privateKey, sha256Hex)` — signs a bottle's SHA-256 digest, returns a
  base64 signature for embedding in the formula YAML.
- `Verify(publicKey, sha256Hex, sig)` — verifies a single signature.
- `LoadTrustedKeys(grewRoot)` — reads `etc/trusted-keys` (SSH or hex format).
- `VerifyAny(keys, sha256Hex, sig)` — checks a signature against any trusted key.

### `sandbox`
macOS Seatbelt sandboxing for builds, archive extraction, and post-install
scripts. Generates a per-invocation Seatbelt profile that denies network access
and confines writes to the minimum required directories.
- `Command(BuildConfig, ...)` — build sandbox (read-only keg write target, compiler temps).
- `PostInstallCommand(PostInstallConfig, ...)` — post-install sandbox (keg is
  read-only; only a scratch TmpDir is writable).
- `ExtractCommand(ExtractConfig, ...)` — extraction sandbox (StageDir only).
- `IsSandboxed()` — reports whether OS-level sandboxing is in effect.
Falls back to a clean environment on non-macOS or when `sandbox-exec` is absent.

### `snapshot`
Per-file integrity manifests stored as `.MANIFEST.json` inside each keg.
- `Capture(kegDir)` — walks the keg and records each file's path, SHA-256,
  SHA-512, size, and mode.
- `Verify(kegDir)` — re-hashes every file and diffs against the stored manifest;
  reports missing, added, and modified entries.
Uses `fsutil.WriteFileAtomic` for crash-safe manifest writes.

---

## Package format

### `formula`
Parses formula YAML definitions. Resolves bottle URLs for the current
platform/OS version, collects build and runtime dependencies, and maps
Homebrew bottle naming conventions to grew's internal model.
Provides `LoadAll()` for comprehensive listing of all locally-available formulae.

### `cask`
Parses cask YAML definitions. Extracts app bundles, binary stubs, and install
targets from macOS-specific cask specs. Provides `Caskroom` for managing
installed casks and their `.MANIFEST.json` snapshots.
Provides `LoadAll()` for comprehensive listing of all locally-available casks.

---

## Storage & linking

### `cellar`
Manages the `Cellar/` directory tree where formula kegs live.
- `kegDir(name)` — constructs the keg path via `safepath.SafeJoin`.
- `KegPath(name, version)` — validates the resolved path stays within the Cellar
  (lexical + symlink-resolved check).
- `RunCleanup` — removes old keg versions and orphaned cache entries.

### `linker`
Creates and removes symlinks in `bin/`, `lib/`, `include/`, and `opt/` for
installed kegs. All entry names are validated with `safepath.SafePathComponent`
before `filepath.Join`.

### `receipt`
Generates and loads `INSTALL_RECEIPT.json`. Records provenance
(`built_from_source` vs `poured_from_bottle`), install timestamp, dependency
snapshot, build options, and `installed_on_request`. Supplemental to
`.MANIFEST.json`; explicitly excluded from `grew verify` to avoid false positives.

### `relocation`
Rewrites hardcoded dylib paths (Mach-O `install_name_tool`) and ELF `RPATH`
entries inside a keg at install time, so bottles work without
`DYLD_LIBRARY_PATH` or `LD_LIBRARY_PATH` hacks.

---

## Downloads

### `downloader`
HTTP download engine with security-first defaults.
- HTTPS-only; HTTP URLs are rejected at parse time.
- SSRF-protected via a host allowlist (`HOMEGREW_ALLOWED_HOSTS`).
- `ComputeHashes(path)` — single-pass SHA-256 + SHA-512 via `io.MultiWriter`.
- `BatchDownload` — concurrent downloads with per-file hash verification.
- `extractTar` / `extractZip` — archive extraction with safepath validation on
  every entry path.

### `release`
Helpers for interacting with grew's own GitHub Releases.
- `FindAssetURL` — locates a named asset in a release.
- `FindAllChecksums` — parses `checksums.txt` into a hash-length → hex map.
- `ExtractBinaryFromFile` — pulls the `grew` binary out of a release archive.
- `AtomicInstall` — writes a binary via `fsutil.WriteFileAtomic`.

### `bpatch`
Multi-hop binary delta update engine. Uses BFS over release assets to find the
shortest sequence of `.patch` files from the current version to the target, then
applies them sequentially with `bspatch`. Each patch is dual-hash verified before
application; the final binary is re-verified against `binary-checksums.txt`.

### `cache`
Download cache management: lookup, store, and age-based pruning
(`HOMEGREW_CLEANUP_MAX_AGE_DAYS`, `--prune=<days>`, `--prune=all`).

---

## Diagnostics & scanning

### `doctor`
Context-driven diagnostic engine invoked by `grew doctor`.
- `BaseChecks` — verifies directory permissions, symlink targets, HTTPS URLs,
  and snapshot integrity.
- `ExtraChecks` — platform-specific checks registered via `init()` (macOS:
  App Sandbox entitlement, notarization via `spctl`, quarantine attributes).
Adding new checks means registering them, not editing the core run loop.

### `osvdev`
OSV.dev REST API client. Queries for known CVEs by package ecosystem + version.
Used by `grew vulnscan` and as a pre-update OSV guard in `grew selfupdate`
(fail-closed: aborts the update if OSV is unreachable or returns an error).

### `depgraph`
Dependency graph construction and topological sort (Kahn's algorithm). Detects
cycles and produces install order. Used by `grew install` and `grew deps`.

### `linkage`
Inspects dynamic library dependencies of installed kegs (Mach-O `otool -L` on
macOS, `ldd` on Linux). Identifies broken links and libraries that fall outside
the grew prefix. Exposed as `grew linkage`.

---

## System integration

### `service`
Background service management — `launchd` plists on macOS, `systemd` units on
Linux. Handles start/stop/restart/status and generates service definition files.
`ExecStart` and plist values are properly escaped to prevent injection.

### `quarantine`
Applies and removes `com.apple.quarantine` extended attributes on downloaded
apps and binaries via a minimal embedded Swift snippet, ensuring Gatekeeper
protection is active. Also provides safe Trash integration via LaunchServices.

### `sudo`
Runs a command under `sudo -A` with a graphical AppleScript askpass prompt on
macOS. The executable is resolved at the call site before being passed to sudo,
so no shell-expanded string is ever passed to `exec.Command`.

### `tap`
Tap repository lifecycle: `git clone`, `git pull`, auto-tapping (clone on demand
when a `user/repo/formula` path is referenced), and commit signature
verification (`HOMEGREW_TAP_VERIFY`).

---

## CLI layer

### `cli`
Shared CLI bootstrap: initializes the cobra root command, registers all `cmd/`
subcommands, sets up logging, and wires context injection. Entry point for
`root.go`.

### `cmd`
Legacy command bridge: high-level orchestration for commands that predate the
`cmd/<name>` modular split. Thin wrappers that delegate to `pkg/` packages.

---

## Utilities

### `auditlog`
Tamper-evident append-only log of grew operations (install, upgrade, uninstall,
tap, self-update). Written to `var/log/grew-audit.log` under the prefix.

### `homebrew`
Homebrew JSON API client. Fetches formula and cask metadata from
`formulae.brew.sh` when no local tap definition is found. Maps Homebrew
platform/bottle naming to grew's internal model.

### `installer`
Top-level installation orchestrator. Coordinates formula/cask installs (bottle
download → hash verify → sandbox extract → link → receipt) and the self-update
flow (OSV check → patch/download → health check → atomic replace).

### `lockfile`
Reproducible environment pinning. `grew lock` records every installed formula
with exact version, SHA-256, and full dependency tree; `grew lock --check`
verifies the current install matches the lockfile.

### `logger`
CLI-friendly `log/slog` handler. Formats DEBUG/INFO/WARN/ERROR lines for
terminal output, adds source file and line number at DEBUG level, and respects
the `-v`/`-d`/`-q` flags from `pkg/flags`.

### `ui`
Zero-dependency ANSI color and TTY detection. Used throughout grew for
Homebrew-style arrow and checkmark output. No-ops on non-TTY output so piped
commands stay clean.

### `validation`
Input validation utilities: package names, version strings, SHA-256/512 hex
digests, URL schemes, and path components. Used at system boundaries (user
input, formula parsing, API responses).

### `version`
Embeds the `main.buildVersion` linker variable and exposes `Version()` and
`BuildInfo()`. Also provides `Compare` for semver-aware version ordering.
