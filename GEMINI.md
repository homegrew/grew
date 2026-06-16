# GEMINI.md

## Purpose

This repository contains `grew`, a Go-based macOS package manager inspired by Homebrew, with a strong emphasis on deterministic installs, secure update flows, clean symlink management, reproducible environments, and polished CLI UX.

This document serves as the developer rules, context reference, and system design guide for Gemini when working in this codebase.

---

## Product Shape & Commands

`grew` is a production CLI product, not a generic experiment. It supports formula and cask installations with SHA256/SHA512 verification, seatbelt sandboxing, and tap automation.

All user-facing subcommands are implemented under `cmd/` as standalone packages exporting a `Command *cobra.Command` and are registered in `pkg/cli/cli.go` inside the `AddCommands` function.

### Core CLI Command Surface

*   **Lifecycle & Management**:
    *   `setup`: One-time system prefix setup (supports `--unsafe` in devmode).
    *   `install`: Installs formulas (from bottles or source) and casks.
    *   `uninstall`: Uninstalls formulas or casks.
    *   `reinstall`: Uninstalls and installs a package from scratch.
    *   `upgrade`: Upgrades packages to their latest version.
    *   `update`: Refreshes local tap definitions and triggers the secure binary self-update (`runSelfUpdate`) check.
    *   `cleanup`: Removes old package versions and prunes cache files older than a threshold.
    *   `resetupdate`: Resets update and self-update state.
*   **Queries & Inspection**:
    *   `search`: Searches for formulas and casks.
    *   `info`: Displays metadata, dependencies, and installation details for a package.
    *   `list`: Lists all installed packages.
    *   `desc`: Performs name and description lookups with substring and `/regex/` matching.
    *   `deps`: Performs dependency tree inspection and topological sorting.
    *   `uses`: Shows installed formulas that depend on a given package.
    *   `leaves`: Lists installed formulas that are not depended on by any other package.
    *   `outdated`: Lists installed packages with available updates.
    *   `missing`: Checks installed kegs for absent runtime dependencies; exits non-zero if any are missing.
*   **Security & Diagnostics**:
    *   `vuln-scan`: Scans installed packages for security vulnerabilities, querying OSV.dev and verifying manifest integrity/permissions.
    *   `verify`: Verifies integrity of installed kegs against their `.MANIFEST.json` snapshots.
    *   `doctor`: Runs environment and layout diagnostic checks (Darwin checks verify App Sandbox, notarization, and extended quarantine attributes).
    *   `linkage`: Analyzes Mach-O or ELF binary linkage.
    *   `audit`: Audits formula and cask definitions for quality and security compliance.
    *   `sign`: Signs formula hashes with Ed25519 keys for verification against the trusted store.
*   **Environment & Utilities**:
    *   `shellenv`: Generates shell exports for setting up PATH and environment variables.
    *   `config`: Prints configuration values and paths grew is currently using.
    *   `alias`: Manages user command shortcuts (e.g. `grew alias add i install`).
    *   `lock`: Manages reproducible lockfiles.
    *   `casks` / `formulae`: Lists all locally available casks or formulas.
    *   `homepage`: Opens a package's homepage in the browser.
    *   `services`: Manages background services for formulas.
    *   `version`: Prints current version details.

---

## Architecture

The codebase follows a modular Go layout where CLI routing is cleanly separated from implementation logic:

*   **Entry Points**: `main.go` and `root.go` initialize the root command.
*   **CLI Subcommands (`cmd/`)**: Each subcommand is in its own subdirectory (e.g., `cmd/install/`) as a separate package. It exports a package-scoped `Command *cobra.Command` and contains a `doc.go` documenting the command. Commands must be registered by adding a single line in `pkg/cli/cli.go` under `AddCommands()`.
*   **Internal Library (`pkg/`)**: Contains the backing packages where the core business logic resides:
    *   `pkg/context`: The single source of truth for execution context.
    *   `pkg/installer`: Installs and configures bottles, casks, and updates.
    *   `pkg/cellar` & `pkg/cask`: Manages directory structures under the prefix.
    *   `pkg/linker`: Handles symlink creation and conflict resolution.
    *   `pkg/safepath` & `pkg/fsutil`: Contain path verification and atomic filesystem utilities.
*   **Developer & Maintenance Tools (`tools/`)**:
    *   `genrepo`: Converts Homebrew JSON API definitions into `grew` YAML formulas and casks.
    *   `patcher`: Generates and verifies binary delta patches (`bsdiff`) for binary releases.

### Execution Context (`pkg/context`)

Never read global variables or environment settings directly in subcommands. All operations receive a `Context` or `InstallContext` (the latter manages global locks for destructive actions). The context encapsulates:
*   `Paths`: System directories (`Cellar`, `Caskroom`, `Taps`, `Cache`, `Log`, `Etc`).
*   `Loader` / `CaskLoader`: Loaders for resolving formula and cask definitions (handling auto-tapping and Homebrew API fallbacks).
*   `Cellar` / `Caskroom`: Managers tracking what is physically installed.
*   `Config`: User preferences and runtime flags.

---

## Engineering & Security Principles

When making changes, prioritize **security before convenience**, and **determinism before implicit behavior**.

### Security Model & Primitives

1.  **Path Safety (`pkg/safepath`)**: All filesystem inputs (from zip files, taps, or external arguments) must be validated via `pkg/safepath`. Use `SafeJoin(base, components...)` to prevent Zip-Slip and path-traversal attacks.
2.  **Atomic Writes (`pkg/fsutil`)**: Write all metadata (receipts, lockfiles, manifests) atomically via `fsutil.WriteFileAtomic` to avoid corrupted half-writes.
3.  **Dual-Hash Verification (`pkg/downloader`)**: Downloads are verified against **both** SHA-256 and SHA-512 computed in a single pass using `io.MultiWriter`.
4.  **SSRF Protection**: Downloads only connect to HTTPS URLs, and are validated against `HOMEGREW_ALLOWED_HOSTS` to prevent SSRF vulnerabilities targeting internal metadata or service endpoints.
5.  **macOS Seatbelt Sandboxing (`pkg/sandbox`)**: Builds, extractions, and post-install scripts are sandboxed with Seatbelt profiles that restrict network egress and write locations.
6.  **Quarantine Enforcement**: Casks and binaries are automatically tagged with macOS quarantine attributes (`com.apple.quarantine`) during extraction.
7.  **Command Execution Hardening**: Resolve binaries via `exec.LookPath`, avoid shell interpreters, and always append the `--` end-of-options separator to prevent command injection.

---

## Update and Install Flows

*   **Formula Installation**: Resolves dependencies using topological sorting (`pkg/depgraph`), checks for cycles, downloads bottles, applies Mach-O path relocations (`install_name_tool`), links files, registers a `.MANIFEST.json` for integrity validation, and saves an `INSTALL_RECEIPT.json` to record explicit intent (`InstalledOnRequest`).
*   **Symlink Linking (`pkg/linker`)**: Exposes Cellar executables/libraries. Linker enforces ownership tracking (only overwriting symlinks owned by the same formula) and prevents version-family conflicts (e.g. refusing to link `node@24` binaries if the unversioned `node` is already linked).
*   **Self-Update Flow**: Triggered during `grew update`.
    *   *Source-based*: Compiles local clone if `Grew` git repo exists under the prefix.
    *   *Release-based*: Fetches releases, queries OSV.dev for CVE gates (fail-closed if unreachable), constructs a Breadth-First Search (BFS) patch chain to apply delta updates (`bspatch`), falls back to full binary download if necessary, executes a sandboxed pre-replacement health check, and swaps the binary atomically.

---

## Code Style & Testing Expectations

### Code Style

*   Write idiomatic Go.
*   Keep functions focused, explicit, and document their behavior.
*   Prefer `log/slog` for structured logging.
*   If code changes invalidate user docs or command help messages, update `cmd/*/doc.go`, `README.md`, and `docs/tech.md` in the same change.

### Testing Architecture

*   **Unit Tests**: Located inside package subdirectories (e.g., `pkg/linker/*_test.go`). Unit tests **require the `devmode` build tag** to compile.
*   **Integration, Smoke, and E2E Tests**: Found in the `tests/` directory. Since features like sandboxing and self-update execute the binary on disk, tests compile a proxy binary from `tests/testbin/` to simulate real-world actions against mock prefixes.

```bash
make build              # Release build → ./grew
make dev                # Build with 'devmode' tag (required for local rootless development)
make test-unit          # Run unit tests (go test -tags devmode -race)
make test-integration   # Run command-level integration tests
make test-smoke         # Run smoke tests
make test-e2e            # Run full lifecycle end-to-end tests (downloads real formulas)
make check-all          # Runs unit + integration + smoke + e2e tests
make lint               # Runs golangci-lint
make fmt                # Runs go fmt ./...
make distclean          # Prune build artifacts after a build
```

To run a single unit test:
```bash
go test -tags devmode -race -run TestName ./pkg/<package>
```

---

## Local Development Setup (Rootless)

Production installations require `sudo` to setup system prefixes. For local development and testing, you can use **Developer Mode** to set up a rootless installation under `~/.homegrew`:

```bash
make dev                  # Compiles in the devmode code path (build tag)
./grew setup --unsafe     # Activates devmode prefix setup at runtime
./grew install jq         # Works without root privileges
```

*Note: Release binaries compiled without the `devmode` build tag ignore the `--unsafe` flag completely.*

---

## Notes for Gemini

When working in this repository, always observe the following constraints:
*   **Read before editing**: Check existing files in `pkg/` and `cmd/` to match conventions.
*   **Preserve the package-manager mental model**: `grew` is a production CLI tool; prioritize safety and determinism.
*   **Execution context**: Never bypass `pkg/context` with global state or hardcoded paths. Always use `ctx.Paths` or config mappings.
*   **Path & Download operations**: All paths must route through `pkg/safepath` and all downloads through `pkg/downloader`.
*   **Adding commands**: Follow the convention in `cmd/README.md`. Create a new directory under `cmd/`, export the `Command` variable, add a `doc.go` file, and register it inside `pkg/cli/cli.go`.
*   **Do not weaken safeguards**: Do not bypass sandboxing, dual-hash verification, or path traversal guards.
