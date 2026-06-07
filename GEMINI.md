# GEMINI.md

## Purpose

This repository contains `grew`, a Go-based macOS package manager inspired by Homebrew, with a strong emphasis on deterministic installs, secure update flows, clean symlink management, reproducible environments, and polished CLI UX.

## Product shape

`grew` is not a generic experiment. It is a full CLI product with:

- Formula and cask installation.
- Tap support and auto-install of missing taps.
- Deterministic linking and opt symlinks.
- Dependency resolution and tree inspection.
- Lockfile support for reproducible environments.
- Doctor, audit, linkage, verify, cleanup, cache, and config tooling.
- Self-update logic with binary patching, hash verification, and vulnerability checks.
- Security-oriented install behavior including sandboxing, quarantine handling, signature verification, and archive extraction hardening.

When making changes, preserve the project as a serious package manager first and a convenience CLI second.

## Architecture

The codebase is organized in a conventional Go CLI layout:

- `main.go` and `root.go` wire the application entrypoint and root command.
- `cmd/` contains user-facing commands, generally one package per command.
- `pkg/` contains reusable internal implementation packages.
- `docs/` holds architecture notes, comparison material, roadmap information, and technical documentation; in particular, `docs/tech.md` should be treated as a key reference for project internals and lifecycle behavior.
- `tests/` contains broader test assets and integration-oriented material.
- `tools/` contains development and release-support utilities.

The command surface is broad. Before changing behavior, inspect both the corresponding `cmd/<name>/` package and the relevant implementation packages in `pkg/`.

## Command conventions

The repository follows a consistent command model:

- Each command usually has a dedicated package under `cmd/`.
- Many commands have a `doc.go` file that documents intent and expected user-facing behavior.
- User-visible behavior should stay aligned with those command docs and with the README.
- Hidden or internal-only commands should remain clearly separated from public UX.

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

- SHA256 and SHA512 verification for downloaded assets.
- Signed bottle and tap verification.
- Vulnerability scanning and fail-closed checks where documented.
- Sandboxed builds and restricted post-install execution.
- Zip Slip and path traversal protection during extraction.
- Safer external command execution patterns.
- macOS-specific quarantine and platform safety behavior.

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
- Preserve structured logging patterns and user-friendly terminal output.

When touching public behavior, also consider shell completions, help text, README references, and tests.

## Documentation discipline

This repository relies on multiple sources of truth that should stay in sync:

- `README.md` for user-facing capabilities and onboarding.
- `cmd/*/doc.go` for per-command intent.
- `docs/tech.md` for technical architecture and lifecycle details.
- `docs/comparison.md` and `docs/ROADMAP.md` for positioning and planned direction.

If code changes invalidate docs, update the docs in the same change.

## Testing expectations

The repository has substantial package-level test coverage across many internal packages. Maintain that standard.

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

1. Format code with `gofmt`.
2. Run focused tests, then broader tests as needed.
3. Update docs and help text if behavior changed.
4. Double-check that security and determinism guarantees were not weakened.

## Useful commands

- Build: `make build`
- Test all packages: `go test ./...`
- Run focused tests: `go test ./pkg/...` or `go test ./cmd/...`
- Format files: `gofmt -w <files>`

## Notes for Gemini

When working in this repository:

- Read before editing.
- Preserve the package-manager mental model.
- Treat setup, install, linking, extraction, and self-update code as high-risk surfaces.
- Keep changes minimal, reviewable, and well-tested.
- Prefer correctness, safety, and maintainability over novelty.
