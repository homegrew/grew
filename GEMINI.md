# GEMINI.md

## Purpose

This repository contains `grew`, a Go-based macOS package manager inspired by Homebrew, with a strong emphasis on deterministic installs, secure update flows, clean symlink management, reproducible environments, and polished CLI UX.

## Product shape

`grew` is not a generic experiment. It is a full CLI product with:

- Formula and cask installation with SHA256 verification, kind auto-detection, and `--formula`/`--cask` pins.
- Name and description lookup and search (`desc`), including substring and `/regex/` matching across both kinds.
- Tap support and auto-install of missing taps.
- Multi-hop binary delta self-update (`selfupdate`) via `bspatch`, dual-hash (SHA256+SHA512) verification, OSV.dev vulnerability gate, and sandboxed pre-replacement health check.
- Ed25519 bottle signing verified against a local trust store; optional tap commit signature enforcement.
- Deterministic linking with opt symlinks, version-family conflict detection, and dry-run support.
- Keg relocation — rewrites hardcoded library paths in bottles at install time via `install_name_tool`.
- Per-file `.MANIFEST.json` install snapshots and `grew verify` integrity checking.
- `INSTALL_RECEIPT.json` provenance and dependency metadata per keg.
- Dependency resolution with topological sort, cycle detection, and tree inspection.
- Lockfile support for reproducible environments.
- Doctor, audit, linkage, verify, cleanup, cache, and config tooling.
- `grew missing` — checks installed kegs for absent runtime dependencies; exits non-zero.
- `grew uses` — shows installed formulae that depend on a given package.
- `grew desc` — name/description lookup with `/regex/` support across formulae and casks.
- `grew outdated` — lists installed packages with available updates; `--formula`/`--cask`/`--json`/`--minimum-version`.
- `grew autoremove` — transitively removes orphaned auto-installed dependencies in one pass.
- `grew leaves` — lists installed formulae not depended on by any other.
- `grew casks` / `grew formulae` — list all locally installable packages with names and descriptions.
- `grew vuln-scan` — queries OSV.dev for known CVEs across installed packages.
- Security-oriented install behavior: macOS Seatbelt sandboxing, quarantine attributes, Zip Slip protection, path-traversal hardening, HTTPS enforcement, and SSRF-protected downloads.

When making changes, preserve the project as a serious package manager first and a convenience CLI second.

## Architecture

The codebase is organized in a conventional Go CLI layout:

- `main.go` and `root.go` wire the application entrypoint and root command.
- `cmd/` contains user-facing commands — each in its own package exporting a `Command` variable and a `doc.go`. These are registered via `pkg/cli.AddCommands()`.
- `pkg/cmd/` contains legacy commands being phased out, added via `pkg/cmd.AddLegacyCommands()`.
- `pkg/` contains the reusable internal implementation packages (`pkg/installer`, `pkg/cellar`, `pkg/formula`, `pkg/cask`, `pkg/linker`, `pkg/context`, etc.).
- `docs/` holds architecture notes, comparison material, roadmap information, and technical documentation; in particular, `docs/tech.md` should be treated as a key reference for project internals and lifecycle behavior.
- `tests/` contains broader test assets: `integration/`, `smoke/`, `e2e/`, `testbin/` (proxy binary source), `testhelper/`.
- `tools/` contains `genrepo` (Homebrew JSON → grew YAML converter) and `patcher` (binary delta generator/verifier).

**Execution context (`pkg/context`)** is the single source of truth for system state. All commands receive a `Context` or `InstallContext` (the latter holds the global lock for destructive ops) rather than reading global state. It bundles `Paths`, `Loader`/`CaskLoader`, `Cellar`/`Caskroom`, and `Config`. `LoadFormula`/`LoadCask` encapsulate auto-tapping and Homebrew API fallback. `ResolveKind(name, forceCask, forceFormula)` returns `(isCask bool, err error)` for commands that accept either kind.

The command surface is broad. Before changing behavior, inspect both the corresponding `cmd/<name>/` package and the relevant implementation packages in `pkg/`.

## Command conventions

The repository follows a consistent command model:

- Each command has a dedicated package under `cmd/<name>/` exporting a `Command *cobra.Command` and a `doc.go`.
- To add a new command: create `cmd/<name>/`, export `Command`, add `doc.go`, and register it with a single line in `pkg/cli.AddCommands()`. See `cmd/README.md`.
- User-visible behavior should stay aligned with `cmd/*/doc.go`, README, and `docs/tech.md`.

When adding or changing commands:

- Update command help text and docs together.
- Keep naming consistent with existing commands and aliases.
- Preserve compatibility unless there is a strong reason to break behavior.
- Prefer explicit flags and predictable output over magical shortcuts.

## Engineering priorities

The project consistently favors the following priorities:

1. Security before convenience.
2. Determinism before implicit behavior.
3. Clear CLI UX before implementation cleverness.
4. Reproducibility and inspectability before opaque automation.
5. Graceful fallback paths instead of brittle assumptions.

This should guide trade-offs. If a change makes the tool feel more magical but less inspectable or less safe, it is probably the wrong trade.

## Security model

Security-sensitive behavior is central to this repository, not incidental. Preserve and respect features such as:

- SHA256 and SHA512 dual-hash verification for all downloaded assets (computed in one pass via `io.MultiWriter`).
- Ed25519 bottle signing verified against `etc/trusted-keys`; optional tap commit signature enforcement.
- OSV.dev vulnerability gate in self-update (fail-closed when OSV is unreachable).
- Sandboxed builds and restricted post-install execution (macOS Seatbelt, `sandbox-exec`).
- Sandboxed pre-replacement health check before atomic binary swap in `selfupdate`.
- Zip Slip and path traversal protection during extraction (`pkg/safepath`).
- HTTPS enforcement at URL parse time; SSRF-protected host allowlisting (`HOMEGREW_ALLOWED_HOSTS`).
- Safer external command execution: `exec.LookPath`, `--` end-of-options separator, no shell string construction.
- macOS quarantine attributes applied to all downloaded apps and binaries.
- `pkg/fsutil.WriteFileAtomic` for all metadata writes (manifest, receipt, lockfile).

Do not remove or weaken safeguards just to simplify code paths. If a change affects trust, verification, or update logic, document the reasoning and review adjacent flows carefully.

## Update and install flows

Several core flows deserve extra caution because they are easy to break subtly:

- `setup` bootstrapping and prefix initialization.
- Formula install and dependency resolution.
- Cask install behavior and artifact handling.
- Link/unlink/relink flows and opt symlink maintenance.
- Cleanup and autoremove semantics.
- Lockfile generation and environment reproduction.
- Self-update, delta patching, binary replacement, and rollback/fallback behavior.

For these areas, prefer small changes, verify edge cases, and read surrounding docs in `docs/tech.md` before modifying behavior.

## Code style

Match the existing style of the repository:

- Write idiomatic Go.
- Keep functions focused and explicit.
- Prefer small helpers and clear naming over deeply clever abstractions.
- Avoid introducing unnecessary frameworks or large dependency expansions.
- Keep error messages actionable and CLI-oriented.
- Preserve structured logging patterns (`log/slog`) and user-friendly terminal output.

When touching public behavior, also consider shell completions, help text, README references, and tests.

## Documentation discipline

This repository relies on multiple sources of truth that should stay in sync:

- `README.md` for user-facing capabilities and onboarding.
- `cmd/*/doc.go` for per-command intent.
- `docs/tech.md` for technical architecture and lifecycle details.
- `docs/ROADMAP.md` for planned and completed direction.
- `cmd/README.md` for command registration conventions.

If code changes invalidate docs, update the docs in the same change.

## Testing expectations

The repository has substantial package-level test coverage across many internal packages. Maintain that standard.

Unit tests require the `devmode` build tag. Integration and smoke tests compile a proxy binary from `tests/testbin/` and exec it against a mock prefix — they cannot use the Go test runner as the current binary.

When changing behavior:

- Run targeted tests for affected packages first.
- Add or update unit tests for bug fixes and new logic.
- Prefer deterministic tests over timing-sensitive or environment-fragile ones.
- Be cautious with macOS-specific behavior and ensure platform assumptions are explicit.

## Practical workflow

Before editing:

1. Read the relevant command package in `cmd/`.
2. Read the backing implementation in `pkg/`.
3. Check README and architecture docs for user and system expectations.
4. Identify tests that should protect the change.

After editing:

1. Format code with `gofmt` or `make fmt`.
2. Run focused tests, then broader tests as needed.
3. Update docs and help text if behavior changed.
4. Double-check that security and determinism guarantees were not weakened.

## Useful commands

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

Run a single test: `go test -tags devmode -race -run TestName ./pkg/<package>`. The `devmode` build tag is required for all unit tests.

### Local development setup (rootless)

Two gates must BOTH be met to install to `~/.homegrew` without root:

```bash
make dev                  # compiles in the devmode code path (build tag)
./grew setup --unsafe     # activates it at runtime (--unsafe flag)
./grew install jq         # now works without root
```

Release binaries ignore `--unsafe` entirely.

## Notes for Gemini

When working in this repository:

- Read before editing.
- Preserve the package-manager mental model.
- Treat setup, install, linking, extraction, and self-update code as high-risk surfaces.
- Keep changes minimal, reviewable, and well-tested.
- Prefer correctness, safety, and maintainability over novelty.
- New commands go in `cmd/<name>/` with a `Command` export and `doc.go`; register in `pkg/cli.AddCommands()`.
- Never bypass `pkg/context` with global state or hardcoded paths — use `ctx.Paths.Cellar` etc.
- All path operations must route through `pkg/safepath`; all downloads through `pkg/downloader`.
