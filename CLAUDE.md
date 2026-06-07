# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`grew` is a macOS-focused, Go-based package manager — a hardened, deterministic alternative to Homebrew. It installs Homebrew-format formulas (CLI tools) and casks (GUI apps) with added security: Ed25519 bottle signing, dual-hash (SHA256/SHA512) verification, sandboxed builds/post-install scripts (macOS Seatbelt), per-file install manifests, and mandatory macOS quarantine attributes.

## Commands

```bash
make build              # release build → ./grew
make dev                # build with 'devmode' tag (required for rootless local install/testing)
make test-unit          # unit tests (go test -tags devmode -race, excludes ./tests)
make test-integration   # command-level integration tests (./tests/integration)
make test-smoke         # quick health checks (./tests/smoke)
make test-e2e           # full lifecycle, installs real formulas from GitHub — several minutes
make check-all          # unit + integration + smoke + e2e
make lint               # golangci-lint run
make fmt                # go fmt ./...
```

Run a single test: `go test -tags devmode -race -run TestName ./pkg/<package>`. Note: unit tests require the `devmode` build tag.

### Local development setup (rootless)

Release builds require `sudo` to set up the system prefix. For local dev, two gates must BOTH be met to install to `~/.homegrew` without root:

```bash
make dev                  # compiles in the devmode code path (build tag)
./grew setup --unsafe     # activates it at runtime (--unsafe flag)
./grew install jq         # now works without root
```

Release binaries ignore `--unsafe` entirely. See [docs/tech.md](docs/tech.md) §5 for the build-constraint mechanism (`pkg/runtime/devmode_on.go` vs `devmode_off.go`).

## Architecture

**Entry point:** [main.go](main.go) → [root.go](root.go). `root.go` builds the `Grew` cobra root via `pkg/cli` (`InitializeRootCommand`, `AddCommands`) and `pkg/cmd` (`AddLegacyCommands`).

**Modular CLI:** Every subcommand is its own package under `cmd/<name>/`, exporting a `Command` variable of type `*cobra.Command` plus a `doc.go` with a package-level description. The CLI layer stays thin — business logic lives in `pkg/` packages (`pkg/installer`, `pkg/cellar`, `pkg/formula`, `pkg/cask`, etc.). To add a command: create `cmd/<name>/`, export `Command`, add `doc.go`, register in `root.go`. See [cmd/README.md](cmd/README.md) and [cmd-creation-skill.md](cmd-creation-skill.md).

**Execution context (`pkg/context`) — the single source of truth for system state.** All commands receive a `Context` (read-only ops) or `InstallContext` (destructive ops, holds global lock) rather than reading global variables. It bundles `Paths` (Cellar, Caskroom, Taps, Cache), `Loader`/`CaskLoader`, and `Cellar`/`Caskroom` managers. `LoadFormula`/`LoadCask` encapsulate auto-tapping (cloning a missing `user/repo` tap on demand) and falling back to the Homebrew JSON API when no local definition exists. **Never bypass this with global state or hardcoded paths** — use `ctx.Paths.Cellar` etc.

**Prefix layout** (`/opt/homegrew` on Apple Silicon, `/usr/local/homegrew` on Intel, `~/.homegrew` in devmode): `Cellar/` (installed kegs, each with `.MANIFEST.json`), `Taps/`, `bin//lib//include/` (symlinks), `opt/` (per-formula keg symlinks), `etc/trusted-keys` (Ed25519 pubkeys). The system prefix deliberately isolates sandboxed builds from `$HOME`.

**Self-update (`grew selfupdate`)** is multi-layered (see [docs/tech.md](docs/tech.md) §2): source-based (git fetch + `go build`) if a repo exists at `<prefix>/Grew`, otherwise multi-hop binary delta patching via `bspatch` (BFS over intermediate patches), with full-archive fallback. Every asset is dual-hash verified; an OSV.dev vulnerability check and a sandboxed health-check run of the new binary both fail closed before atomic replacement.

**Per-package metadata:** `.MANIFEST.json` is the canonical per-file SHA256 integrity snapshot used by `grew verify`; `INSTALL_RECEIPT.json` is supplemental provenance/dependency metadata (and is excluded from verify to avoid false positives). The `InstalledOnRequest` field distinguishes explicit installs from auto-pulled dependencies — this drives `grew leaves` and `grew autoremove`.

**Diagnostics (`pkg/doctor`):** Context-driven checks. Core `BaseChecks` plus platform-specific `ExtraChecks` registered via `init()` (e.g. `doctor_darwin.go` adds sandbox-entitlement, notarization, and quarantine checks). Add new checks by registering, not by editing the core flow.

## Security conventions (enforced throughout)

- **External commands:** always pass `--` end-of-options separator to `git`, `systemctl`, `launchctl`, `hdiutil`, `tar`, etc. Avoid shells; use positional parameters for namespace setup.
- **Paths:** validate/normalize via `pkg/safepath` and `pkg/validation`; path-traversal and Zip Slip protection are expected at cellar, linker, loader, and extraction layers.
- **HTTPS** is enforced at parse time (HTTP URLs rejected before download). Downloads go through SSRF-protected host allowlisting (`HOMEGREW_ALLOWED_HOSTS`).
- **Least privilege:** `sudo` only for initial prefix setup; all runtime ops are rootless.

## Testing architecture

Integration and smoke tests can't use the Go test runner as the "current binary" (features like sandboxed extraction and self-update call `os.Executable`). Instead they compile a proxy binary from `tests/testbin/` that exposes grew's full internal routing, then exec it as a standalone process against a mock prefix. See [tests/README.md](tests/README.md). Shared helpers live in `tests/testhelper/`.

## Key env vars

`HOMEGREW_PREFIX` (root, inferred from binary location), `HOMEGREW_TAP_VERIFY` (`off`/`warn`/`strict` — git commit signature policy), `HOMEGREW_ALLOWED_HOSTS`, `HOMEGREW_CLEANUP_MAX_AGE_DAYS`, `HOMEGREW_NO_INIT_TAP` (skip core-tap init, used in tests).

## Repo maintenance tools (`tools/`)

`genrepo` converts Homebrew JSON API formulas/casks into grew YAML. `patcher` generates/verifies binary delta patches between releases (`bsdiff`, dual-hash, `-U` to verify a patch sequence without generating). See [docs/tech.md](docs/tech.md) §3.
