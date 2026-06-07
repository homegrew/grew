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
*   [ ] **`grew bump-formula-pr`**: A command to automate version bumps (updating version strings and SHAs) and optionally submitting a pull request to the core tap.

## 3. Remote Metadata API
Improve performance and reduce initial installation friction by removing the strict dependency on a local git clone.
*   [x] **API-Driven Search & Info**: Allow `grew search` and `grew info` to query a remote JSON API (similar to `formulae.brew.sh`) when a local tap is absent or outdated.
*   [ ] **`HOMEGREW_API_DOMAIN`**: Add configuration support to override the default metadata endpoint.

## 4. Automated Maintenance
*   [ ] **Auto-Update Cooldown**: Introduce an automatic, lightweight `grew update` check before commands (e.g., install/upgrade) if the tap hasn't been updated in a configurable interval (defaulting to 24 hours).
*   [x] **Enhanced Cleanup**: `grew cleanup --prune=<days>` removes cache entries older than a given threshold (or `--prune=all` to clear everything).

## 5. Cask Enhancements
*   [ ] **Deep Uninstall (`grew zap <cask>`)**: Implement thorough removal of Cask artifacts, including configuration files, caches, and `Application Support` directories.
*   [ ] **`.pkg` Support**: Fulfill the existing TODO in `pkg/cask/cask.go` to support `.pkg` installers via the macOS `installer` command.

## 6. Quality of Life Commands
*   [x] **`grew homepage <formula>`**: Launch the formula or cask's `homepage` URL in the default system browser.
*   [x] **`grew uses <formula>`**: Display all installed formulae that depend on the specified package.
*   [ ] **`grew missing`**: Detect broken dependency chains or missing required libraries on the local system.
