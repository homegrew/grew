# Architecture & Technical Details

This document explains some of the internal mechanics of `grew`, specifically focusing on bootstrapping, updates, and developer workflows.

## 1. How `grew` installs itself

Because `grew` manages dependencies and system environments, its initial installation needs to be deterministic and safe.

**The Installation Flow:**

1. **Download the Binary:** The user downloads the appropriate pre-compiled binary for their platform (e.g., `grew_Darwin_arm64.tar.gz`) from the [GitHub Releases](https://github.com/homegrew/grew/releases/latest) page and extracts it.
2. **System Setup:** The user runs the extracted binary using `./grew setup`. The `setup` command initializes the system prefix (`/opt/homegrew` on Apple Silicon, `/usr/local/homegrew` on Intel), prompting for elevated privileges if needed to create the directory and transfer ownership to the current user.
3. **Binary Installation:** By default, `setup` downloads the latest official `grew` binary release and installs it into `<prefix>/bin/grew`. This ensures that all standard installations benefit from pre-built, signed, and verified binaries. Users can opt-in to a source-based installation (using `git clone` and `go build`) by passing the `--unsafe` flag.
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

2. **Multi-Hop Binary Patching (Delta Update)**:
   - `grew` dynamically constructs a patch path using a Breadth-First Search (BFS) to find the shortest sequence of intermediate patch assets (e.g., `v0.1.0_to_v0.1.1`, `v0.1.1_to_v0.2.0`) to reach the latest release if a direct patch isn't available.
   - **Tooling**: If a continuous sequence of patches is found and `bspatch` is available in the system `PATH`, only the required deltas are downloaded.
   - **Sequential Application & Verification**: Each `.patch` file is downloaded, verified against SHA-256 and SHA-512 hashes in the release metadata, and applied sequentially using `bspatch`.
   - **Post-Patch Verification**: The final reconstructed binary is verified against the `binary-checksums.txt` file of the target release (distinct from `checksums.txt`, which contains hashes for the compressed archives and patches). This ensures the reconstruction was 100% accurate across all hops.

3. **Full Download Fallback**:
   - If `bspatch` is missing, no compatible patch is found, or the patching process fails, `grew` falls back to downloading the full platform-specific archive (e.g., `grew_Darwin_arm64.tar.gz`).
   - The archive is extracted, and the binary is staged.

4. **Cryptographic Integrity**:
   - `grew` performs **Dual-Hash Verification**: all downloaded assets (patches or archives) and the final reconstructed binary are verified against both **SHA-256** and **SHA-512** hashes. This protects against supply-chain attacks targeting a single algorithm.

5. **Pre-Replacement Health Check**:
   - Before completing the update, `grew` executes the newly generated binary with the `vuln-scan --offline` command inside a restricted sandbox.
   - This verifies that the binary is structurally sound, compatible with the host OS, and functionally operational (i.e., not a corrupted file or a "zero-day" bricking binary) before it replaces the stable version.
   - For release builds, it also re-executes with `--version` to confirm the reported version string matches the expected tag.

6. **Atomic Replacement**:
   - The final, verified binary is moved to `<prefix>/bin/grew` using an atomic rename operation.

## 3. Repository Maintenance Tools

The `homegrew/grew` repository includes tools for maintaining the formula and cask ecosystem:

### `genrepo`
A unified tool used to bootstrap and maintain the core formula and cask repositories by importing definitions from the Homebrew JSON API.
- **Formula Import**: Fetches Homebrew formulas, maps platforms, picks appropriate bottles, and generates `grew`-compatible YAML files.
- **Cask Import**: Fetches Homebrew casks, extracts macOS-specific app/binary artifacts, and converts them into `grew` YAML format.
- **Consistency**: It utilizes the internal `grew` domain models to ensure the generated definitions are valid and follow current schema standards. The output leverages `omitempty` serialization to produce clean, concise YAML without empty fields.

### `patcher`
A developer tool used to generate binary delta patches between releases.
- **Automation**: Downloads existing releases from GitHub and extracts the raw binaries.
- **Delta Generation**: Uses `bsdiff` to compute the minimal patch required to transition from an old version to a new one.
- **Integrity**: Automatically calculates SHA-256 and SHA-512 hashes for the resulting patch files, formatted for inclusion in the release's `checksums.txt`.
- **Platform Aware**: Handles mapping between internal OS/Architecture names and the naming conventions used in release assets.

## 4. Diagnostic Engine (`pkg/doctor`)

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
    - **Quarantine Attributes:** Confirms that macOS malware checks haven't been inadvertently stripped by verifying extended attributes via `xattr` (though `grew` applies these attributes and manages uninstallation trashing natively using embedded Swift scripts to ensure proper LaunchServices registration).

This architecture allows developers to easily add new checks without modifying the core execution flow, ensuring `grew` can continuously expand its health and security validations.

## 5. Developer Mode (`devmode`) Explained

Typically, `grew` requires root privileges (`sudo grew setup`) during initial setup to create system-level prefix directories (like `/opt/homegrew`), establishing a strict isolation boundary between the package manager and the user's `$HOME` directory.

However, requiring `sudo` is a major friction point for local development, testing, and continuous integration workflows. To solve this, `grew` includes a **Developer Mode**.

**What is `devmode`?**
Devmode is a combination of a compile-time build tag and a runtime CLI flag that enables user-local, rootless installations.

**How it works:**
1. **Compile-time Gate:** You must compile the binary with the `devmode` build tag: `go build -tags devmode`. In the codebase, this tag triggers the inclusion of `internal/runtime/devmode_on.go`, which sets the constant `runtime.DevMode = true`. This works via mutually exclusive Go build constraints (`//go:build devmode` vs `//go:build !devmode`), so only one of these files is compiled in any given build. (Release builds therefore include `devmode_off.go`, where `runtime.DevMode = false`).
2. **Runtime Gate:** You must pass the `--unsafe` flag to the setup command: `./grew setup --unsafe`.
3. **Evaluation:** When `grew` initializes, `runtime.devModeActive()` checks that *both* conditions are met (`DevMode && Unsafe`).

If devmode is active, `grew` bypasses the standard `sudo` requirements and instead sets the prefix to a hidden directory in the user's home folder: `~/.homegrew`. This allows developers to test the full lifecycle of the package manager—including sandboxed extraction, cellar linking, and dependency resolution—without ever escalating privileges or modifying system directories.

*Note: Release builds ignore the `--unsafe` flag entirely. If a user attempts to run `grew setup --unsafe` on a production binary, it will fail and demand `sudo`.*

## 6. Installation Metadata (`INSTALL_RECEIPT.json`)

To provide rich information about installed packages beyond what is strictly necessary for integrity verification (which is handled by `.MANIFEST.json`), `grew` generates an `INSTALL_RECEIPT.json` file during the final stages of installation.

This receipt is stored directly within the keg directory (e.g., `<prefix>/Cellar/jq/1.6/INSTALL_RECEIPT.json`) and captures runtime and build-time metadata:

- **Provenance:** Records whether the package was `built_from_source` or `poured_from_bottle`.
- **Timestamps:** Records the exact `installed_at` time.
- **Dependencies:** Snapshots the `dependencies` and `runtime_dependencies` required by the package.
- **Build Environment:** Can optionally record the `compiler` used and specific `build_options` if compiled locally.
- **Intent:** Captures `installed_on_request` to distinguish between explicit user installs and automatic dependency resolution.

This metadata powers commands like `grew info`, allowing users to inspect the exact configuration and provenance of their installed packages. In short, `.MANIFEST.json` is the canonical integrity snapshot used by `grew verify`, while `INSTALL_RECEIPT.json` is supplemental operational metadata for inspection and dependency reasoning. Because the receipt is generated *after* the initial filesystem snapshot, it is explicitly ignored by the `grew verify` integrity checks to prevent false positives.

## 7. Dependency Management & Cleanup

`grew` tracks why a package was installed using the `InstalledOnRequest` field in its receipt and manifest. This distinction is critical for maintaining a lean system:

- **Installed on Request:** The package was explicitly requested by the user (e.g., `grew install jq`).
- **Installed as Dependency:** The package was pulled in automatically to satisfy the requirements of another formula.

This metadata enables precise identification of "orphaned" dependencies—packages that were installed automatically but are no longer required by any currently installed formula.

### Identifying Leaves and Orphans
- **`grew leaves`**: Lists all packages that are not dependencies of any other installed package.
    - `-r`, `--installed-on-request`: Filters to show only top-level packages you explicitly wanted.
    - `-p`, `--installed-as-dependency`: Filters to show orphaned dependencies that are likely safe to remove.
- **`grew autoremove`**: Automatically uninstalls orphaned dependencies. It performs a calculation to find packages that are both "leaves" and have `InstalledOnRequest: false`.
    - **Safe by Default**: Packages explicitly installed by the user are never removed by `autoremove`, even if they are not dependencies of anything else.
## 8. Modular CLI Architecture

Starting with version 0.5.0, `grew` transitioned to a modular CLI architecture. Subcommands are no longer monolithic within a single package. Instead, each command resides in its own standalone package under the `cmd/` directory (e.g., `cmd/install`, `cmd/upgrade`).

**Key Benefits:**
- **Standardization:** Every subcommand package exports a consistent `Command` variable of type `*cobra.Command`.
- **Isolation:** Each command manages its own flags and dependencies, reducing the risk of unintended side effects and global state pollution.
- **Unified Context:** All commands utilize a centralized execution context defined in `internal/context`. This package provides the `Context` (for read-only operations) and `InstallContext` (for destructive operations, including global locking) types, ensuring consistent environment resolution.
- **Decoupled Logic:** Core management logic is separated from CLI orchestration. High-level commands in `cmd/` delegate complex operations to dedicated packages:
    - `internal/installer`: Handles formula, cask, and self-update routines.
    - `internal/cellar`: Manages installed packages and disk cleanup.
    - `internal/formula` & `internal/cask`: Handle definition parsing and metadata.
- **Testability:** Standalone packages enable targeted unit testing and mocking without pulling in the entire CLI surface area.

The CLI entry point in `main.go` and the root command definition in `root.go` utilize the `internal/cli` package to import these standalone packages and register them into the primary `Grew` root command.

