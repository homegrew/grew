# Architecture & Technical Details

This document explains some of the internal mechanics of `grew`, specifically focusing on bootstrapping, updates, and developer workflows.

## 1. How `grew` installs itself

Because `grew` manages dependencies and system environments, its initial installation needs to be deterministic and safe. There are two supported first-time installation paths.

### Path A — Native macOS installer (`.pkg` / `.dmg`)

The release page ships a `Homegrew.dmg` containing a `Homegrew Installer.pkg`. This is the recommended installation method for end users.

1. **Payload delivery:** The package drops a universal `grew` binary at `/private/tmp/homegrew-setup/grew` via the standard Installer payload mechanism. The binary is built with `lipo` from separate `arm64` and `amd64` builds so a single artifact works on any Mac.
2. **`postinstall` script:** After the payload lands, macOS Installer runs `tools/installer/postinstall` as root:
   - Detects the host architecture via `uname -m` and selects the correct prefix (`/opt/homegrew` on Apple Silicon, `/usr/local/homegrew` on Intel).
   - Creates the full prefix directory structure (same set as `pkg/config/paths.go InitDirs()`, minus user-scoped external dirs like `~/Applications` and `~/Library/Caches/Homegrew`).
   - Moves the binary from the temporary payload drop to `<prefix>/bin/grew`.
   - Writes `/etc/paths.d/homegrew` containing `<prefix>/bin`, so macOS `path_helper` adds grew to `PATH` for new login shells — no manual shell profile edit required for basic use.
   - Transfers ownership of the entire prefix to the real logged-in user (obtained from `/dev/console`) with group `admin`, matching the behaviour of `setupSystem()` in `cmd/setup/setup.go`.
   - Removes the temporary payload directory.
3. **No `grew setup` needed:** The installer performs all the steps that `grew setup` would, so users can proceed directly to adding `eval "$(grew shellenv)"` to their profile for full env var export (`HOMEGREW_PREFIX`, `MANPATH`, etc.).

The `tools/build-installer.sh` script in the repository builds both artifacts from the current source tree:

```bash
./tools/build-installer.sh [--version <ver>] [--output-dir <dir>]
# Produces: dist/Homegrew Installer.pkg  and  dist/Homegrew.dmg
```

### Path B — Universal binary + `grew setup`

1. **Download the Binary:** The user downloads the appropriate pre-compiled binary for their platform (e.g., `grew_Darwin_arm64.tar.gz`) from the [GitHub Releases](https://github.com/homegrew/grew/releases/latest) page and extracts it.
2. **System Setup:** The user runs the extracted binary using `./grew setup`. The `setup` command initializes the system prefix, prompting for elevated privileges if needed to create the directory and transfer ownership to the current user.
3. **Binary Installation:** By default, `setup` downloads the latest official `grew` binary release and installs it into `<prefix>/bin/grew`. Users can opt-in to a source-based installation (using `git clone` and `go build`) by passing the `--unsafe` flag.
4. **Permissions:** After creating the prefix and moving the binary, `setup` transfers ownership of the directory structure to the current user (if started via `sudo`, it uses the `SUDO_USER` environment variable). All subsequent `grew` commands run without root privileges.

## 2. How the update process works

`grew`'s self-update mechanism (`grew selfupdate`) is designed to atomically replace the running binary without leaving the package manager in a broken state. It prioritizes security and efficiency through a multi-layered verification process.

The update strategy diverges based on how `grew` was initially set up:

**Source-Based Updates (Primary Strategy)**
If a valid git repository exists at `<prefix>/Grew` (created during `setup`), the update proceeds via source compilation:
1. `grew` invokes `git fetch` to retrieve the latest changes from the origin repository.
2. If `HOMEGREW_TAP_VERIFY` is enabled, it verifies the cryptographic signatures of the remote commits.
3. It performs a fast-forward merge via `git reset --hard origin/main`.
4. It executes `go build -o <prefix>/bin/grew .` inside the repository.
5. The executable is atomically replaced.

**Release-Based Updates (Fallback & Binary Patching)**
If the source repository does not exist, `grew` attempts an optimized binary update:

1. **Discovery & Vulnerability Check**:
   - `grew` queries the GitHub API for the latest stable release.
   - **OSV.dev Guard**: Before downloading any assets, `grew` queries the [OSV.dev](https://osv.dev) database for the target version. If the new version has known critical vulnerabilities, the update is aborted.
   - **Availability Controls**: OSV queries use a bounded timeout and short retry sequence to reduce failures from transient network issues.
   - **Default Policy (Fail-Closed)**: If OSV remains unreachable, times out, or returns an invalid/error response after retries, the update aborts by default and asks the user to retry once connectivity is restored.
   - **Manual Override**: For emergency scenarios, users may explicitly bypass the OSV gate via a documented override flag/environment setting. This is opt-in, emits a prominent warning, and should only be used when users accept the risk of updating without live vulnerability intelligence.

2. **Multi-Hop Binary Patching (Delta Update)**:
   - `grew` dynamically constructs a patch path using a Breadth-First Search (BFS) to find the shortest sequence of intermediate patch assets (e.g., `v0.1.0_to_v0.1.1`, `v0.1.1_to_v0.2.0`) to reach the latest release if a direct patch isn't available.
   - **Tooling**: If a continuous sequence of patches is found and `bspatch` is available in the system `PATH`, only the required deltas are downloaded.
   - **Sequential Application & Verification**: Each `.patch` file is downloaded, verified against its SHA-256 and SHA-512 entries in the release `checksums.txt` file, and then applied sequentially using `bspatch`.
   - **Post-Patch Verification**: The final reconstructed binary is verified against the `binary-checksums.txt` file of the target release (distinct from `checksums.txt`, which contains hashes for the compressed archives and patches). This ensures the reconstruction was 100% accurate across all hops.

3. **Full Download Fallback**:
   - If `bspatch` is missing, no compatible patch is found, or the patching process fails, `grew` falls back to downloading the full platform-specific archive (e.g., `grew_Darwin_arm64.tar.gz`).
   - The archive is extracted, and the binary is staged.

4. **Cryptographic Integrity**:
   - `grew` performs **Dual-Hash Verification**: all downloaded assets (patches or archives) and the final reconstructed binary are verified against both **SHA-256** and **SHA-512** hashes. This protects against supply-chain attacks targeting a single algorithm.

5. **Pre-Replacement Health Check**:
   - Before completing the update, `grew` executes the newly generated binary with the `vuln-scan --offline` command inside a restricted sandbox (temporary working directory, no elevated privileges, strict outbound network isolation when supported by the host OS, and a short execution timeout).
   - Network egress control is implemented using platform-specific OS primitives where available. On platforms that **support** strict outbound blocking, `grew` enforces it and fails closed if enforcement unexpectedly fails. On platforms that do **not** provide strict egress isolation primitives, `grew` surfaces a high-visibility warning and requires explicit user confirmation (or an equivalent opt-in flag) before running this pre-replacement check with reduced isolation.
   - This verifies that the binary is structurally sound, compatible with the host OS, and functionally operational (i.e., not a corrupted file or a "zero-day" bricking binary) before it replaces the stable version.
   - For release builds, it also re-executes with `--version` to confirm the reported version string matches the expected tag.

6. **Atomic Replacement**:
   - The final, verified binary is moved to `<prefix>/bin/grew` using an atomic rename operation.

## 3. Security Primitives

Several sections above reference dual-hash verification, sandboxing, and atomic replacement. Those behaviors are not re-implemented per call site — they live in a small set of shared packages that the rest of the codebase is expected to route through. Consolidating them here is deliberate: a single hardened implementation is easier to audit than the same logic copy-pasted across the installer, linker, and extractor.

### Path safety (`pkg/safepath` + `os.OpenRoot`)

Every filesystem operation that touches an externally-influenced path (archive entries, tap contents, asset names, cellar paths) is validated through `pkg/safepath` before the path reaches `os.Open`, `os.Rename`, or `filepath.Join`. This is the single layer responsible for path-traversal and Zip-Slip protection.

- **`SafeJoin(base, components...)`**: joins components onto `base` and returns an error if the result escapes `base`. This is the preferred primitive — it sanitizes at the join site rather than validating after the fact, which is also the form recognized by static taint analysis.
- **`CheckSubpath(base, target)`** / **`IsSubpath(base, target)`**: assert that an already-constructed `target` lies within `base` (used where the path is built elsewhere, e.g. before deleting a keg).
- **`CleanPath(path)`**: rejects `..` traversal markers and returns the cleaned path.
- **`SafePathComponent(name)`**: validates a single filename component — no separators, null bytes, or `..`.
- **`SafeAbsolutePath(path)`**: requires the path to be absolute, clean, and not the filesystem root.

String-level path checks close the *lexical* traversal window but cannot eliminate the TOCTOU race between a directory walk's `lstat` and the subsequent `open` — an adversary can swap a regular file for a symlink in that window. Keg directory walks in `pkg/linkage`, `pkg/relocation`, `pkg/fsutil`, and `pkg/cellar` therefore use `os.OpenRoot(kegPath)` (Go 1.23+) to open a root directory file descriptor and then walk via `fs.WalkDir(root.FS(), ".")`. All subsequent path operations (`root.Open`, `root.OpenFile`, `root.Readlink`) are routed through `openat(2)` against that fd, so symlinks inside a keg that point to paths outside it cannot be followed regardless of the timing of the walk — the containment is enforced at the OS level, not in userspace.

### Atomic writes (`pkg/fsutil`)

`fsutil.WriteFileAtomic(dst, data, mode)` is the canonical way to write metadata files. It writes to a temporary file in the destination directory, applies the mode, closes, then `rename`s into place — so a reader never observes a half-written file and a crash mid-write cannot corrupt an existing one. The self-update binary replacement, snapshot manifests (`pkg/snapshot`), and install receipts (`pkg/receipt`) all go through it instead of carrying their own temp-file dance. `pkg/fsutil` also provides `CopyTree`/`CopyFileWithinRoot` (root-confined copies), advisory file locking, and `SanitizeMode` (strips setuid/setgid/sticky/world-write bits during extraction).

### Dual-hash computation (`pkg/downloader`)

`downloader.ComputeHashes(path)` reads a file once and computes its SHA-256 and SHA-512 simultaneously via `io.MultiWriter`, returning both hex digests. Dual hashing protects against a supply-chain attack that targets a weakness in a single algorithm; computing both in one pass avoids reading large artifacts twice. The downloader also enforces HTTPS-only fetches behind an SSRF-protected host allowlist (`HOMEGREW_ALLOWED_HOSTS`) and rejects redirects to non-HTTPS targets.

**`HOMEGREW_ALLOWED_HOSTS` details (SSRF control):**
- **Format:** comma-separated hostnames (optionally with `:port`), for example: `github.com,api.github.com,objects.githubusercontent.com`.
- **Configurability:** the `HOMEGREW_ALLOWED_HOSTS` environment variable is user-configurable at runtime; if set, it overrides the built-in/default allowlist.
- **Default behavior:** when unset, `grew` uses a conservative built-in list containing only the hosts required for official release/download/update flows.
- **Security implications:** expanding this list increases the outbound destinations `grew` may contact. Avoid wildcards or broad internal domains; doing so weakens SSRF protections and can expose internal services/metadata endpoints if untrusted input ever reaches download URLs.

### Symlink conflict management (`pkg/linker`)

`pkg/linker` creates and removes the prefix symlinks that expose installed kegs through shared directories (`bin/`, `lib/`, `include/`, `share/`, `opt/`). It implements two safety mechanisms to prevent version-family conflicts:

1. **Ownership tracking**: When linking, the linker only replaces an existing symlink if it already points into the same formula's cellar subtree. Links owned by a different formula cause an error unless `LinkOpts.Overwrite` is explicitly set.

2. **Version-family conflict guard** (defense-in-depth): Refuses to link a formula into shared directories when another member of the same version family is already linked. For example, when `node@24` attempts to link `bin/node`, the linker checks if an unversioned `node` is already providing that link. This is a backstop that protects against scenarios where a formula definition explicitly sets `keg_only: false` despite being versioned — the check ensures the version family's uniqueness regardless of the formula definition. The `Overwrite` or `Force` option overrides this check.

See [pkg/linker/doc.go](../pkg/linker/doc.go) for complete documentation of linking semantics, including keg-only behavior and path safety.

### Command-execution hardening

External tools (`git`, `go`, an editor from `$EDITOR`, etc.) are resolved with `exec.LookPath` before being passed to `exec.Command`, and every external invocation passes the `--` end-of-options separator so that attacker-influenced arguments cannot be reinterpreted as flags. Where a value such as `$EDITOR` is involved, it is additionally screened for shell metacharacters before resolution (see [cmd/alias/alias.go](../cmd/alias/alias.go)). No runtime path constructs a shell command string.

### Build & post-install sandboxing (`pkg/sandbox`)

On macOS, builds, archive extraction, and post-install scripts run under `sandbox-exec` with dynamically generated Seatbelt profiles. Each profile denies network access and restricts writes to the minimum set of directories the step legitimately needs:

- **`BuildCommand`** (`BuildConfig`): writable build and keg directories plus the compiler's temp areas; reads allowed broadly for system headers.
- **`PostInstallCommand`** (`PostInstallConfig`): keg is read-only, only a scratch `TmpDir` is writable — post-install scripts cannot mutate the installed files.
- **`ExtractCommand`** (`ExtractConfig`): writes confined to a staging directory.

The environment is scrubbed to a minimal allowlist before each invocation. On non-macOS hosts (or where Seatbelt is unavailable) the commands still run with a cleaned environment, but without OS-enforced isolation; `IsSandboxed()` reports which regime is in effect. This is the mechanism behind the self-update pre-replacement health check in §2.

### Ed25519 bottle signing (`pkg/signing`)

`pkg/signing` implements detached Ed25519 signatures over a bottle's SHA-256 digest. `grew sign` ([cmd/sign](../cmd/sign)) signs a formula's hashes with a private key (raw hex seed or an unencrypted OpenSSH Ed25519 key) and emits YAML to paste into the definition. At verification time, `truststore.LoadTrustedKeys(<prefix>/etc/trusted-keys)` loads the operator-trusted public keys (SSH or hex format) and `signing.VerifyAny` checks a bottle's signature against any of them. `grew vulnscan` uses this to flag installed formulae that are unsigned or whose signature no longer matches.

## 4. Repository Maintenance Tools

The `homegrew/grew` repository includes tools for maintaining the formula and cask ecosystem:

### `genrepo`
A unified tool used to bootstrap and maintain the core formula and cask repositories by importing definitions from the Homebrew JSON API.
- **Formula Import**: Fetches Homebrew formulas, maps platforms, picks appropriate bottles, and generates `grew`-compatible YAML files.
- **Cask Import**: Fetches Homebrew casks, extracts macOS-specific app/binary artifacts, and converts them into `grew` YAML format.
- **Consistency**: It utilizes the internal `grew` domain models to ensure the generated definitions are valid and follow current schema standards. The output leverages `omitempty` serialization to produce clean, concise YAML without empty fields.
- **Build overrides**: Homebrew encodes build logic as arbitrary Ruby (`def install`), so details such as "the configure script lives under `unix/`" cannot be derived from the JSON API. `overrides.go` holds a per-formula `build:` override table (`formulaBuildOverrides`) that reinstates these for the formulas grew builds from source (e.g. `tcl-tk → working_dir: unix`). Overrides only fill fields the conversion left empty.

### `patcher`
A developer tool used to generate and verify binary delta patches between releases.
- **Automation**: Downloads existing releases from GitHub and extracts the raw binaries.
- **Delta Generation**: Uses `bsdiff` to compute the minimal patch required to transition from an old version to a new one.
- **Integrity**: Automatically calculates SHA-256 and SHA-512 hashes for the resulting patch files, formatted for inclusion in the release's `checksums.txt`.
- **Platform Aware**: Handles mapping between internal OS/Architecture names and the naming conventions used in release assets.
- **Verification (`-U`)**: Validates that an unbroken, verifiable sequence of patches exists between two versions and that all intermediate hashes match the expected `checksums.txt` values without generating new patches.

## 5. Diagnostic Engine (`pkg/doctor`)

`grew` includes a comprehensive diagnostic engine designed to verify the health, security, and structural integrity of an installation. This engine is invoked via the `grew doctor` command.

The diagnostic system is designed with modularity and extensibility in mind:

- **Context-Driven Execution:** Diagnostics run against a shared `Context` object that holds the current state of the system, including loaded formulas, casks, installed packages, and configuration paths. This minimizes redundant filesystem reads and parses.
- **Core Checks:** A suite of foundational checks (`BaseChecks`) ensures that:
    - Critical directories exist and are not world-writable.
    - Symlinks in the `bin/` directory point to valid targets within the prefix.
    - Download URLs enforce HTTPS and cryptographic hashes are valid.
    - Installations have valid `snapshot` manifests and haven't been tampered with.
- **Platform-Specific Extensions:** The system uses `init()` functions to register platform-specific checks into an `ExtraChecks` slice. For example, on macOS, `doctor_darwin.go` injects checks that verify:
    - **App Sandbox:** Ensures installed casks possess the `com.apple.security.app-sandbox` entitlement.
    - **Notarization:** Uses `spctl` to verify that applications pass Gatekeeper assessment.
    - **Quarantine Attributes:** Confirms that macOS malware checks haven't been inadvertently stripped by verifying extended attributes via `xattr` during diagnostics. Separately, `grew` applies and manages these attributes during installation and uninstallation (including trashing) using embedded Swift scripts to ensure proper LaunchServices registration.

This architecture allows developers to easily add new checks without modifying the core execution flow, ensuring `grew` can continuously expand its health and security validations.

## 6. Developer Mode (`devmode`) Explained

Typically, `grew` requires root privileges (`sudo grew setup`) during initial setup to create system-level prefix directories (like `/opt/homegrew`), establishing a strict isolation boundary between the package manager and the user's `$HOME` directory.

However, requiring `sudo` is a major friction point for local development, testing, and continuous integration workflows. To solve this, `grew` includes a **Developer Mode**.

**What is `devmode`?**
Devmode is a combination of a compile-time build tag and a runtime CLI flag that enables user-local, rootless installations.

**How it works:**
1. **Compile-time Gate:** You must compile the binary with the `devmode` build tag: `go build -tags devmode`. In the codebase, this tag triggers the inclusion of `pkg/runtime/devmode_on.go`, which sets the constant `runtime.DevMode = true`. This works via mutually exclusive Go build constraints (`//go:build devmode` vs `//go:build !devmode`), so only one of these files is compiled in any given build. (Release builds therefore include `devmode_off.go`, where `runtime.DevMode = false`).
   > **Warning:** A binary built with `-tags devmode` must be treated as a development-only artifact and must never be distributed as an official release. If such a binary is accidentally shipped, users can enable relaxed setup behavior by passing `--unsafe`, weakening normal production safeguards. Release pipelines should enforce this with explicit CI checks (forbidden build tags), reproducible release scripts that pin production flags, and artifact validation steps that confirm `devmode` is not enabled.
2. **Runtime Gate:** You must pass the `--unsafe` flag to the setup command: `./grew setup --unsafe`.
3. **Evaluation:** When `grew` initializes, `runtime.devModeActive()` checks that *both* conditions are met (`DevMode && Unsafe`).

If devmode is active, `grew` bypasses the standard `sudo` requirements and instead sets the prefix to a hidden directory in the user's home folder: `~/.homegrew`. This allows developers to test the full lifecycle of the package manager—including sandboxed extraction, cellar linking, and dependency resolution—without ever escalating privileges or modifying system directories.

*Note: Release builds ignore the `--unsafe` flag entirely. If a user attempts to run `grew setup --unsafe` on a production binary, it will fail and demand `sudo`.*

## 7. Installation Metadata (`INSTALL_RECEIPT.json`)

To provide rich information about installed packages beyond what is strictly necessary for integrity verification (which is handled by `.MANIFEST.json`), `grew` generates an `INSTALL_RECEIPT.json` file during the final stages of installation.

This receipt is stored directly within the keg directory (e.g., `<prefix>/Cellar/jq/1.6/INSTALL_RECEIPT.json`) and captures runtime and build-time metadata:

- **Provenance:** Records whether the package was `built_from_source` or `poured_from_bottle`.
- **Timestamps:** Records the exact `installed_at` time.
- **Dependencies:** Snapshots the `dependencies` and `runtime_dependencies` required by the package.
- **Build Environment:** Can optionally record the `compiler` used and specific `build_options` if compiled locally.
- **Intent:** Captures `installed_on_request` to distinguish between explicit user installs and automatic dependency resolution.

This metadata powers commands like `grew info`, allowing users to inspect the exact configuration and provenance of their installed packages. In short, `.MANIFEST.json` is the canonical integrity snapshot used by `grew verify`, while `INSTALL_RECEIPT.json` is supplemental operational metadata for inspection and dependency reasoning. Because the receipt is generated *after* the initial filesystem snapshot, it is explicitly ignored by the `grew verify` integrity checks to prevent false positives.

## 8. Dependency Management & Cleanup

`grew` tracks why a package was installed using the `InstalledOnRequest` field in its receipt and manifest. This distinction is critical for maintaining a lean system:

- **Installed on Request:** The package was explicitly requested by the user (e.g., `grew install jq`).
- **Installed as Dependency:** The package was pulled in automatically to satisfy the requirements of another formula.

This metadata enables precise identification of "orphaned" dependencies—packages that were installed automatically but are no longer required by any currently installed formula.

### Identifying Leaves and Orphans
- **`grew leaves`**: Lists all packages that are not dependencies of any other installed package.
    - `-r`, `--installed-on-request`: Filters to show only top-level packages you explicitly wanted.
    - `-p`, `--installed-as-dependency`: Filters to show orphaned dependencies that are likely safe to remove.
- **`grew autoremove`**: Automatically uninstalls orphaned dependencies in a single invocation. It iterates: each pass recomputes the dependency graph excluding packages already marked for removal, then looks for newly exposed orphans (packages that are now leaves with `InstalledOnRequest: false`). The loop repeats until no new candidates are found, so a full transitive chain (e.g. `root → mid → leaf`, all auto-installed) is removed in one run rather than requiring repeated invocations.
    - **Safe by Default**: Packages explicitly installed by the user are never removed by `autoremove`, even if they are not dependencies of anything else.

### Detecting Broken Dependency Chains
- **`grew missing`**: Checks installed kegs for declared runtime dependencies that are not present in the Cellar — the inverse of `leaves`/`autoremove`, which look for packages no longer *needed*; `missing` looks for packages still needed but *absent*. With no arguments it walks every installed keg (`Cellar.List()`); given names, it checks only those targets. For each keg it loads the formula definition (`ctx.LoadFormula`) and reports any entry in `Dependencies` not present in a pre-computed installed-packages map, printing one `<formula>: <dependency>` line per finding. Only runtime dependencies are considered — build-only dependencies (`BuildDependencies`) are excluded, since their absence does not break an installed package.
    - `--hide=<list>`: Treats a comma-separated list of formulae as if they were not installed, useful for previewing what would break before uninstalling them.
    - **Exit status**: Returns a non-zero exit code when any missing dependency is found, so it can gate scripts and CI checks. Because a non-zero exit is the command's normal "found something" signal rather than a misuse, it suppresses Cobra's automatic usage/help text (the command syntax summary shown on errors) and prints only the offending list.
    - **Casks**: grew casks declare no dependencies, so cask installations are always reported as complete.

## 9. Shell Completion (`pkg/completion`)

`grew` generates shell completion scripts via `grew completion <bash|zsh|fish>` (cobra built-in). For argument completion — suggesting formula or cask names as you type — `grew info` registers a `ValidArgsFunction` that delegates to `pkg/completion.NamesCache`.

### Name list caching

Fetching the full Homebrew formula or cask list on every tab-press would be impractical (~7 MB and ~2 MB respectively). `NamesCache` solves this with a file-based cache under `{cache}/completion/`:

| File | Contents |
|---|---|
| `formula-names.json` | `{"names":[…],"fetched_at":"…"}` |
| `cask-names.json`    | Same for cask tokens |

**TTL:** 24 hours, enforced by checking the file's mtime. On a cache miss (file absent, expired, or corrupt), `NamesCache` calls `homebrew.FetchFormulaNames()` or `homebrew.FetchCaskNames()` and overwrites the cache. Write errors are silently ignored so completion degrades gracefully offline.

### Lightweight API fetch

`FetchFormulaNames` and `FetchCaskNames` in `pkg/homebrew` hit the same `formulae.brew.sh` list endpoints as the full bulk fetchers but parse only the minimal fields needed for filtering (name/token, deprecated, disabled, version). This avoids allocating thousands of full `Formula`/`Cask` structs just to extract names.

### Completion probe performance

`ValidArgsFunction` calls `config.Default()` directly rather than `context.New()`, skipping core-tap initialisation. The probe is therefore fast on a cache hit: it reads a small JSON file and returns.

## 10. Modular CLI Architecture

Starting with version 0.5.0, `grew` transitioned to a modular CLI architecture. Subcommands are no longer monolithic within a single package. Instead, each command resides in its own standalone package under the `cmd/` directory (e.g., `cmd/install`, `cmd/upgrade`).

**Key Benefits:**
- **Standardization:** Every subcommand package exports a consistent `Command` variable of type `*cobra.Command`.
- **Isolation:** Each command manages its own flags and dependencies, reducing the risk of unintended side effects and global state pollution.
- **Unified Context:** All commands utilize a centralized execution context defined in `pkg/context`. This package provides the `Context` (for read-only operations) and `InstallContext` (for destructive operations, including global locking) types, ensuring consistent environment resolution.
- **Decoupled Logic:** Core management logic is separated from CLI orchestration. High-level commands in `cmd/` delegate complex operations to dedicated packages:
    - `pkg/installer`: Handles formula, cask, and self-update routines.
    - `pkg/cellar`: Manages installed packages and disk cleanup.
    - `pkg/formula` & `pkg/cask`: Handle definition parsing and metadata.
- **Testability:** Standalone packages enable targeted unit testing and mocking without pulling in the entire CLI surface area.

The CLI entry point in `main.go` and the root command definition in `root.go` utilize the `pkg/cli` package to import these standalone packages and register them into the primary `Grew` root command.

**Example — `cmd/outdated`:** the `outdated` subcommand was initially a stub inside `cmd/upgrade`. It was extracted into its own `cmd/outdated` package to gain independent flags (`--formula`, `--cask`, `--json`, `--minimum-version`), cask support via `ctx.Caskroom.List()`, and JSON output — without touching the upgrade command's logic.

## 11. Execution Context (`pkg/context`)

The `Context` struct serves as the central registry for shared application state in `grew`. Its primary purpose is to bundle together the various managers and loaders that almost every command needs to function, implementing a pattern of explicit dependency injection.

### Key Components
- **`Paths`**: Contains the configured filesystem locations for the Cellar, Caskroom, Taps, and Cache.
- **`Loader` & `CaskLoader`**: Handle the discovery and parsing of Formulae (CLI tools) and Casks (macOS apps) from local taps or remote APIs.
- **`Cellar` & `Caskroom`**: Provide high-level APIs for managing the directories where packages are actually installed.

### Design Goals
1.  **Lifecycle Management**: The context initialization ensures that system paths are validated and core components (like the default Tap) are ready before any command logic executes.
2.  **Shared Logic**: It hosts cross-cutting methods like `LoadFormula` and `LoadCask`, which encapsulate complex behaviors such as automatic repository tapping (auto-tapping) and falling back to the Homebrew API when a local definition is missing. `ResolveKind(name, forceCask, forceFormula)` returns `(isCask bool, err error)` for commands that accept either kind and need to determine which loader to use without duplicating the formula-wins logic inline.
3.  **Consistency**: By passing a single `Context` object through the command hierarchy, `grew` ensures that all components operate on the same configuration and prefix, preventing environment drift during execution.

## 12. Dependency Resolution & Lifecycle Hooks

`grew` provides structured dependency management with multiple scopes (runtime, build, test, optional, recommended) and supports formula lifecycle hooks for build, test, and post-install phases.

### Dependency Modeling (`pkg/formula`, `pkg/depgraph`)

Each formula declares dependencies with explicit kind annotations:
- **Runtime**: Required when the installed formula is used.
- **Build**: Required only during source compilation, excluded from runtime dependency chains.
- **Test**: Required only for test execution via `grew test`.
- **Optional**: Runtime dep that users may skip if unavailable.
- **Recommended**: Suggested but not required.

Dependencies are represented in two forms:
- **Structured**: `Deps []Dependency` with explicit `Kind` and platform tags for fine-grained control.
- **Legacy**: `Dependencies []string` for backward compatibility with existing formula catalogs.

### Graph Construction & Ordering (`pkg/depgraph`, `pkg/resolver`)

The dependency graph is built by loading formula definitions and populating a `Graph` struct with nodes (as `NodeMeta`) and edges. The resolver performs validation and ordering:

1. **Topological Sorting**: Kahn's algorithm ensures dependency-first ordering. When multiple nodes have zero in-degree, runtime dependencies (`Kind != DepBuild`) are emitted before build-only dependencies, with alphabetical tiebreaking for determinism.
2. **Cycle Detection**: DFS-based detection with exact cycle path reporting (`DetectCycles()`) prevents installing circular dependency chains.
3. **Missing Dependency Check**: The resolver scans all edge targets against the graph's node set; missing nodes return a `MissingError` before attempting topological sort.

### Lifecycle Hooks (`pkg/hooks`)

Formulas can declare lifecycle hooks executed at specific phases during installation:

- **`BuildHooks`**: Executed after a successful source build and before moving into the keg directory. Examples: `make`, `configure`, `make-install`.
- **`TestHook`**: A single hook identifier for test execution. Runs before and after the test phase.
- **`PostInstall`**: Traditionally handled by the `PostInstall` formula field (shell script); now also routed through the hook system for consistency.

Hook execution is sandboxed:
- **Build hooks** run in the build directory with access to the compiler, in the same sandbox as the `make` steps.
- **Post-install hooks** run in a restricted sandbox with the keg read-only; only a temporary directory is writable (no network access, minimal environment).

### Post-Install Caveats (`pkg/caveats`)

Formulas can include a `Caveats` string printed after successful installation. The `Renderer` applies simple template substitution:
- `{{.Formula}}` → formula name
- `{{.Version}}` → version string
- `{{.Prefix}}` → grew prefix directory (e.g., `/opt/homegrew`)

All URLs in caveats are validated; `http://` URLs are rejected at render time to enforce HTTPS-only messaging.

### Cycle Detection in Doctor

The `grew doctor` command includes a `check_depgraph_acyclic` check that loads all formulas from installed kegs and reports any circular dependencies. This ensures that the installed package graph remains resolvable should a user ever attempt to construct a dependency chain manually.

## 13. Formula & Cask Definition System

Grew uses a YAML-based formula and cask definition format, inspired by Homebrew but simplified and extended with additional metadata.

### Formula Structure (`pkg/formula`)

A formula in `grew` is a YAML file defining a CLI tool or library. Key fields include:

```yaml
name: jq
version: 1.7.1
homepage: https://jqlang.github.io/jq/
description: A lightweight and flexible command-line JSON processor
license: MIT
sha256: abc123...

bottles:
  - os: darwin
    arch: arm64
    macos_major: 14
    sha256: def456...
    url: https://github.com/...

dependencies:
  - name: openssl@3
    kind: runtime
  - name: bison
    kind: build
    platform: darwin
    
keg_only: false
post_install: |
  echo "Installed jq"
```

**Key Design Points:**
- **Version-based kegs**: Each version is installed to `Cellar/<name>/<version>/`, allowing side-by-side installation of multiple versions.
- **Bottle selection**: Bottles are matched by `os`, `arch`, and optionally `macos_major` (e.g., macOS 14, 15). The `--force-bottle` option forces a bottle even if it wouldn't normally apply.
- **Platform-specific dependencies**: Dependencies can be tagged with `platform: darwin` to apply only on macOS.
- **Validation**: Formula names and versions are validated via `pkg/validation` to ensure they conform to allowed characters (alphanumerics, `@`, `-`, `_`, `.`).

### Cask Structure (`pkg/cask`)

Casks represent GUI applications and system extensions. They differ from formulas in that they typically contain pre-built `.app` bundles or `.pkg` installers rather than source code.

```yaml
name: firefox
version: 126.0
homepage: https://www.mozilla.org/firefox/
description: Web browser
url: https://download.mozilla.org/firefox/releases/...
sha256: ghi789...

artifacts:
  - app: Firefox.app
    target: /Applications
  - zap:
    - ~/Library/Application Support/Firefox
    - ~/Library/Caches/Firefox
```

**Key Design Points:**
- **Artifact-based**: Casks declare artifacts (apps, packages, binaries) to install, extract, or manage.
- **Zap rules**: Define cleanup targets (files/directories) for deep uninstalls (future `grew zap` command).
- **No keg isolation**: Unlike formulas, cask artifacts are typically installed directly to system locations (e.g., `/Applications`).

### Loader System (`pkg/formula.Loader`, `pkg/cask.Loader`)

The loader provides a unified interface for discovering formulas and casks:

1. **Local Resolution**: Searches installed taps in order. Taps are git-cloned repositories stored under `<prefix>/Taps/`.
2. **Auto-tapping**: If a formula is requested as `user/repo/formula`, the loader automatically clones `https://github.com/user/repo` if it doesn't exist locally.
3. **Homebrew API Fallback**: If a formula isn't found locally, the loader queries the Homebrew JSON API (`formulae.brew.sh`) for metadata and bottle information.
4. **Caching**: Loaded formulas are cached in memory during a single command execution to avoid redundant parses.

## 14. Installation Flow & Cellar Management

The installation process follows a deterministic sequence designed to maximize safety and permit atomic rollback on failure.

### Formula Installation (`pkg/installer.InstallFormula`)

1. **Dependency Resolution**: Recursively load dependencies, construct a dependency graph, detect cycles, and topologically sort for installation order.
2. **Bottle Selection**: 
   - Match the current platform and macOS version against available bottles.
   - Under `--force-bottle`, prefer the newest available bottle even if it predates the current macOS version.
   - Fall back to source build if no suitable bottle is found and source is available.
3. **Bottle Installation** (preferred):
   - Download the bottle archive with SHA-256 verification.
   - Extract to a temporary staging directory within the Cellar (isolated from other kegs).
   - Validate file mode bits; strip dangerous bits (setuid, setgid, sticky, world-write).
   - Perform keg relocation (see §14 below) to rewrite hardcoded prefix paths in binaries.
   - Capture a per-file `.MANIFEST.json` snapshot of installed files and their SHA-256 hashes.
4. **Source Build** (if no bottle or `--build-from-source`):
   - Download the source archive.
   - Extract to a temporary build directory with Seatbelt sandbox isolation.
   - Run `./configure && make && make install DESTDIR=<keg>` (the configure and install commands, plus the working subdirectory, are overridable via the formula's `build:` section).
   - Execute any declared lifecycle hooks with restricted environment and I/O.
5. **Linking**: Call `Linker.LinkWithOpts()` to create symlinks in the prefix, detecting and preventing version-family conflicts.
6. **Post-Install**: Run post-install script (if declared) in a sandbox with the keg read-only.
7. **Receipt Generation**: Write `INSTALL_RECEIPT.json` with metadata (provenance, dependencies, timestamps).
8. **Caveats**: Render and display any post-install messages.

### Cask Installation (`pkg/installer.CaskInstall`)

1. **Artifact Extraction**: Extract `.dmg`, `.zip`, or `.tar.gz` to a temporary directory.
2. **Artifact Movement**: Copy artifact files (e.g., `.app`) to target directories (typically `/Applications`).
3. **Quarantine**: Apply `com.apple.quarantine` extended attributes to downloaded artifacts to trigger macOS Gatekeeper.
4. **Receipt Recording**: Store installation metadata in the Caskroom for future reference.

### Keg Relocation (`pkg/relocation`)

When a bottle is extracted with a hardcoded prefix (e.g., `/usr/local/homegrew`), but the current prefix differs, `grew` rewrites binary paths:

- **Mach-O binaries** (macOS): Use `install_name_tool` to rewrite `@rpath`, `@loader_path`, and absolute paths in dynamic library references.
- **ELF binaries** (Linux, if supported): Use `patchelf` to rewrite `RPATH` and `RUNPATH` entries.
- **Text files** (scripts, configs): Use simple string replacement to swap the old prefix for the new one.

This is a defense-in-depth layer: while bottles should ideally use relative paths or `@rpath`, relocation ensures portability across prefix locations.

### Cellar Management (`pkg/cellar`)

The `Cellar` struct manages installed kegs:

- **`List()`**: Returns all installed kegs, parsed from directory structure.
- **`Installed(name, version)`**: Checks if a specific keg is installed.
- **`Latest(name)`**: Returns the newest version of a formula (by semantic versioning).
- **`Remove(name, version)`**: Atomically deletes a keg directory tree after unlinking.
- **`Cleanup(name, keep=int)`**: Removes old versions, keeping the `keep` newest.

**Manifest Verification**: Each keg carries a `.MANIFEST.json` capturing the exact files and their SHA-256 hashes at install time. The `grew verify` command re-computes hashes and reports mismatches, detecting tampering or corruption.

## 15. Tap Management & Formula Discovery

Taps are git repositories containing formula and cask definitions. The tap system enables community contributions and private package repositories.

### Tap Structure

A tap repository follows this layout:
```
user-repo/
├── Formula/
│   ├── jq.yaml
│   ├── ripgrep.yaml
│   └── ...
├── Cask/
│   ├── firefox.yaml
│   └── ...
├── .git/
└── README.md
```

### Tap Operations (`pkg/tap`)

- **`Clone(user, repo)`**: Clones a GitHub repository to `<prefix>/Taps/user/repo` using `git clone --depth=1` (shallow clone for speed).
- **`Update(name)`**: Runs `git fetch && git reset --hard origin/main` to refresh definitions.
- **Commit Verification** (if `HOMEGREW_TAP_VERIFY=strict` or `warn`): Validates GPG or SSH signatures on commits using `git verify-commit` or equivalent.

### Tap Initialization

On first use, `grew` automatically initializes a "core" tap (typically `homegrew/core`) containing the official formula and cask definitions. This initialization happens in `pkg/context.New()` and is skipped in tests via the `HOMEGREW_NO_INIT_TAP` environment variable.

## 16. Download & Caching System

The download system is designed for resilience, efficiency, and security.

### Cache Structure (`pkg/cache`)

```
<prefix>/Cache/
├── v1/                    ← version namespace
│   ├── formula/
│   │   ├── jq/1.7.1
│   │   │   ├── jq-1.7.1.tar.gz
│   │   │   └── jq-1.7.1.tar.gz.sha256
│   │   └── ...
│   ├── cask/
│   │   ├── firefox
│   │   │   ├── firefox-126.0.dmg
│   │   │   └── ...
│   │   └── ...
│   └── grew/              ← self-update patches
│       ├── v0.1.0_to_v0.1.1.patch
│       └── ...
└── tmp/                   ← ephemeral extraction staging
```

### Download Flow (`pkg/downloader`)

1. **Hash Verification**: Before downloading, check if the file already exists in the cache and verify its SHA-256 (and SHA-512 if available).
2. **Conditional Download**: Skip download if the cache entry is valid; otherwise, fetch from the URL.
3. **HTTPS Enforcement**: URLs are parsed; `http://` URLs are rejected at parse time.
4. **SSRF Protection**: The downloader maintains an allowlist of permitted hosts. User-provided downloads are checked against this list; untrusted input cannot direct the downloader to internal services or metadata endpoints.
5. **Dual-Hash Computation**: After download, compute both SHA-256 and SHA-512 simultaneously to verify integrity and protect against single-algorithm collision attacks.
6. **Redirect Safety**: The HTTP client is configured to reject redirects to non-HTTPS targets, preventing downgrade attacks.

### Cache Cleanup (`pkg/cache.Cleanup`)

The `grew cleanup` command manages cache lifecycle:

- **`--prune=DAYS`**: Remove entries older than `DAYS` days (default: 120 days, configurable via `HOMEGREW_CLEANUP_MAX_AGE_DAYS`).
- **`--scrub` / `-s`**: Remove all cache entries.
- Cleanup also removes old keg versions from the Cellar (keeping the latest 2 by default).

## 17. Logging & Audit Trail

Grew implements structured logging and audit trails to support debugging, compliance, and post-mortem analysis.

### Structured Logging (`pkg/logger`)

All logging uses Go's standard `log/slog` package with a custom handler:

```go
slog.Info("installing formula",
    "formula", "jq",
    "version", "1.7.1",
    "method", "bottle",
    slog.Group("timing",
        "started", start,
        "duration", elapsed,
    ),
)
```

**Log Levels**:
- **ERROR**: Installation failures, missing dependencies, permission denied.
- **WARN**: Fallback to source build, deprecated usage, unsigned bottles.
- **INFO**: Normal progress (install complete, dependency resolved).
- **DEBUG**: Detailed execution steps (file copied, symlink created), source locations.

**CLI Flags**:
- `-v` / `--verbose`: Increase log level to INFO.
- `-d` / `--debug`: Increase to DEBUG (includes source file and line numbers).
- `-q` / `--quiet`: Suppress all output except errors.

### Audit Log (`pkg/auditlog`)

A persistent append-only log records every package manager action:

```
<prefix>/var/log/grew.audit.log

[2024-06-14T15:32:41Z] install jq 1.7.1 bottle sha256=abc123... status=success
[2024-06-14T15:33:12Z] install openssl@3 1.1.1 bottle sha256=def456... status=success
[2024-06-14T15:35:08Z] upgrade jq 1.7.1 → 1.8 bottle sha256=ghi789... status=success
[2024-06-14T15:35:41Z] tap add homegrew/core status=success
```

**Logged Actions**: install, uninstall, upgrade, tap (add/remove), self-update, update (refresh tap definitions).

**Entry Format**: ISO 8601 timestamp, action, formula/cask name, version, bottle hash, final status.

## 18. Error Handling & Resilience

Grew is designed to fail gracefully and provide actionable error messages.

### Error Categories

- **Configuration Errors**: Invalid prefix, missing taps, permission denied. These fail fast at startup.
- **Resolution Errors**: Missing dependencies, cycles in dependency graph, formula not found. Reported clearly with suggestions.
- **Download Errors**: Network failures, checksum mismatch, 404. Retried with exponential backoff; eventually fail closed.
- **Installation Errors**: Build failures, sandbox violations, post-install script errors. Recorded in audit log with full command output for debugging.
- **Linking Errors**: Symlink conflicts, permission denied, existing incompatible link. Reported with guidance (e.g., `--force` to override).

### Atomicity & Rollback

Installation is designed to be atomic:

1. All work happens in isolated staging directories outside the Cellar.
2. The final move into the Cellar is a single atomic `rename()` or `mv` operation.
3. If the process crashes or is interrupted mid-installation, the staging directory is left behind but doesn't corrupt the installed packages.
4. On restart, `grew doctor` can detect and clean up partial installations.

## 19. Testing Architecture

Grew's test suite spans unit tests, integration tests, smoke tests, and end-to-end tests.

### Unit Tests (`make test-unit`)

- Location: `**/*_test.go` in `pkg/` and `cmd/` packages.
- Execution: `go test -tags devmode -race -coverprofile=coverage.out ./pkg/...`
- Scope: Tests individual packages in isolation (e.g., dependency graph validation, YAML parsing, path safety).
- Speed: Runs in seconds; no system setup required.

### Integration Tests (`make test-integration`)

- Location: `tests/integration/`
- Execution: Compiles a test proxy binary from `tests/testbin/` that exposes grew's internal routing, then execs it as a standalone process against a mock prefix.
- Scope: Tests command-level behavior and inter-package interactions (e.g., installing a formula, verifying manifest, unlinking).
- Speed: Runs in ~30–60 seconds; requires mock formulas and tap setup.

### Smoke Tests (`make test-smoke`)

- Location: `tests/smoke/`
- Execution: Quick health checks (e.g., binary builds, version flag works, help text renders).
- Speed: Runs in <5 seconds.

### End-to-End Tests (`make test-e2e`)

- Location: `tests/e2e/`
- Execution: Tests against actual GitHub releases and real formulas from `homegrew/core`.
- Scope: Full lifecycle (download, extract, install, link, verify, cleanup).
- Speed: Several minutes; requires network access and real formulas.

## 20. Codebase Organization & Design Principles

### Package Conventions

- **`pkg/` packages**: Core logic, no CLI routing. Each package is importable and testable in isolation.
- **`cmd/` packages**: CLI commands. Each exports a `Command` variable and a `doc.go` with package documentation.
- **`pkg/cli`**: Command registration and global flag initialization. No business logic.
- **`pkg/context`**: Shared execution context; the single source of truth for system state.

### Design Principles

1. **Explicit Dependency Injection**: Commands and functions receive a `Context` or `InstallContext` rather than reading global state. This ensures testability and prevents environment drift.

2. **Security-by-Default**: All external inputs (URLs, paths, formulae names) are validated at entry points before being used. Path operations route through `pkg/safepath`; downloads through `pkg/downloader`; commands through `exec.LookPath`.

3. **Defense-in-Depth**: Security checks are layered. For example, symlink safety is enforced at the linker, cellar, and loader layers. A single layer's failure doesn't break the whole system.

4. **Minimal Privileges**: The binary runs rootless except for the initial `setup` command. All subsequent operations (install, upgrade, link) operate as the current user within the confined prefix.

5. **Atomic Operations**: Installation, updates, and linking are designed to be atomic—either fully complete or leave no trace.

6. **Fail-Closed**: When in doubt, fail. Examples: OSV.dev vulnerability check aborts updates by default if OSV is unreachable; symlink conflicts raise an error unless explicitly overridden.

7. **Observable**: All significant actions are logged (audit trail), and `grew doctor` provides introspection into system state.

## 21. Directory Structure (`pkg/config`)

`grew` sets up a defined directory structure within its prefix (e.g. `/opt/homegrew`) during installation. Some of the key directories include:

- `Cellar/`: Houses installed formulas, organized by `<name>/<version>`.
- `Caskroom/`: Houses installed casks.
- `Taps/`: Holds cloned git repositories containing formula/cask definitions.
- `bin/`, `sbin/`, `lib/`, `include/`: Standard shared directories where active formulas are symlinked.
- `opt/`: Used for symlinks to active formula versions.
- `var/`: Contains variable state like `tmp/` (staging), `log/` (audit logs), and `locks/` (mutex files).
- `etc/`: Configuration and trusted keys.
- `docs/`: Hosts grew's documentation.
- `manpages/`: Hosts manpages for grew and installed tooling.
- `Frameworks/`: Stores macOS frameworks required by packages.
- `completions/`: Hosts shell completion scripts.
  - `completions/bash/`: Completion scripts for bash.
  - `completions/fish/`: Completion scripts for fish.
  - `completions/zsh/`: Completion scripts for zsh.
- `share/`: Used for shared data and other assets.

