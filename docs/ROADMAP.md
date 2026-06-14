# Grew Feature Roadmap: Homebrew Parity

This document outlines the strategic path to bring `grew` closer to feature parity with Homebrew, focusing on user lifecycle management, developer experience, and system maintenance.

## 1. `grew bundle` (Infrastructure as Code)
Implement declarative environment management via `Grewfile` support.
*   [ ] **`grew bundle dump`**: Generate a `Grewfile` from the current system state (installed formulae and casks).
*   [ ] **`grew bundle install`**: Idempotently install all packages listed in a `Grewfile`.

## 2. Developer Experience Tools
Lower the barrier to entry for contributing new formulae and maintaining existing ones.
*   [x] **`grew create <url>`**: Auto-generate a boilerplate YAML formula. It should attempt to infer the name and version from the URL and automatically calculate the SHA256 checksum.
*   [x] **`grew alias`**: Manage command aliases (`add`, `rm`, `show`, `list`, `edit`) stored in a per-prefix JSON file.
*   [x] **`grew vulnscan`**: Scan installed formulae and casks against OSV.dev for known CVEs.
*   [ ] **`grew edit <formula>`**: Open a formula or cask YAML definition in the user's configured `$EDITOR`.
*   [ ] **`grew cat <formula>`**: Print the raw YAML source of a formula or cask to stdout, without requiring the user to know its tap path.
*   [ ] **`grew log <formula>`**: Show the `git log` history scoped to a formula's definition file within its tap, making it easy to trace version changes and authorship.
*   [ ] **`grew readall [tap]`**: Preflight-load every formula and cask in a tap to catch parse errors and schema violations early; useful in CI for tap maintainers.
*   [ ] **`grew environment <formula>`**: Print the build environment variables (CC, CFLAGS, LDFLAGS, PATH, etc.) that would be set when building a formula, to help debug build failures.
*   [ ] **`grew bump-formula-pr`**: A command to automate version bumps (updating version strings and SHAs) and optionally submitting a pull request to the core tap.

## 3. Package Lifecycle Management
Commands for controlling the download, installation, and version-switching lifecycle.
*   [ ] **`grew fetch <formula|cask> [...]`**: Download bottles and cask artifacts into the cache without installing them. Useful for pre-warming CI caches, air-gapped machines, and bandwidth-limited installs.
*   [ ] **`grew postinstall <formula>`**: Re-run a formula's post-install script and caveats in isolation. Useful after a system restore or when the original install was interrupted.
*   [ ] **`grew switch <formula> <version>`**: Relink a formula to a previously installed keg version already present in the Cellar, without re-downloading or rebuilding. Complements `grew pin` for long-term version management.

## 4. Remote Metadata API
Improve performance and reduce initial installation friction by removing the strict dependency on a local git clone.
*   [x] **API-Driven Search & Info**: Allow `grew search` and `grew info` to query a remote JSON API (similar to `formulae.brew.sh`) when a local tap is absent or outdated.
*   [ ] **`HOMEGREW_API_DOMAIN`**: Add configuration support to override the default metadata endpoint.

## 5. Automated Maintenance
*   [ ] **Auto-Update Cooldown**: Introduce an automatic, lightweight `grew update` check before commands (e.g., install/upgrade) if the tap hasn't been updated in a configurable interval (defaulting to 24 hours).
*   [x] **Enhanced Cleanup**: `grew cleanup --prune=<days>` removes cache entries older than a given threshold (or `--prune=all` to clear everything).

## 6. Cask Enhancements
*   [ ] **Deep Uninstall (`grew zap <cask>`)**: Implement thorough removal of Cask artifacts, including configuration files, caches, and `Application Support` directories.
*   [ ] **`.pkg` Support**: Fulfill the existing TODO in `pkg/cask/cask.go` to support `.pkg` installers via the macOS `installer` command.

## 7. Quality of Life Commands
*   [x] **`grew homepage <formula>`**: Launch the formula or cask's `homepage` URL in the default system browser.
*   [x] **`grew uses <formula>`**: Display all installed formulae that depend on the specified package.
*   [x] **`grew missing`**: Check installed formula kegs for missing runtime dependencies; exits non-zero if any are found. Supports `--hide` to treat a comma-separated list of packages as not installed.
*   [x] **`grew desc [formula|cask|text|/regex/]`**: Display a package's name and one-line description. Supports `-s/--search` (names + descriptions), `-n/--name` (names only), `-d/--description` (descriptions only), `--formula/--cask` kind restriction, `/regex/` patterns, and `--plain` to suppress grouped `==> Formulae`/`==> Casks` headers.
*   [x] **`grew outdated [formula|cask ...]`**: List installed formulas and casks with an updated version available. Supports `--formula`/`--cask` to filter by kind, `--json` for machine-readable output, `--quiet` for names-only output, and `--minimum-version` to filter by a version floor. Extracted from the upgrade command into its own standalone package (`cmd/outdated`).
*   [x] **`grew casks`**: List all locally installable casks with short names and descriptions.
*   [x] **`grew formulae`**: List all locally installable formulae with short names and descriptions.

## 8. Linker & Symlink Management ✅ COMPLETE
Robust prefix symlink management with conflict detection.
*   [x] **Ownership Tracking**: Only replace symlinks owned by the same formula; require `--force` to override
*   [x] **Version-Family Conflict Guard**: Prevent linked members of the same family (e.g., `node@24` when `node` is linked) from both winning shared directories
*   [x] **Linker API Documentation**: Comprehensive [doc.go](../pkg/linker/doc.go) explaining linking semantics, keg-only behavior, and conflict resolution

## 9. Dependency Resolution & Lifecycle Management ✅ COMPLETE
Advanced dependency modeling and formula lifecycle hooks.
*   [x] **Structured Dependencies**: `DepKind` enum with Runtime, Build, Test, Optional, and Recommended scopes
*   [x] **Topological Sort**: Dependency-first ordering with Kahn's algorithm
*   [x] **Cycle Detection**: DFS-based cycle detection with exact path reporting
*   [x] **Lifecycle Hooks**: Pre/post-build, pre/post-test, and post-install hooks with sandboxed execution
*   [x] **Post-Install Caveats**: Template-based rendering with `{{.Formula}}`, `{{.Version}}`, `{{.Prefix}}` substitution
*   [x] **`grew test` Command**: Run formula test hooks in isolation
*   [x] **Doctor Check**: `check_depgraph_acyclic` validates installed keg dependency graph
