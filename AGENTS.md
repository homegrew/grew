# grew AI Agent Instructions

This file helps AI-assisted coding agents understand the grew repository quickly and contribute safely.

## What this repository is

- `grew` is a Go-based macOS-focused package manager inspired by Homebrew.
- It is implemented as a CLI with modular subcommands, sandboxed installs, verified self-updates, and macOS-specific package management features.
- Key user-facing behavior is documented in `README.md` and internal architecture details are in `docs/tech.md`.

## Recommended developer workflow

- Build: `make build`
- Build in developer mode: `make dev`
- Run unit tests: `make test-unit`
- Run all checks: `make check-all`
- Format code: `make fmt`
- Lint code: `make lint`

## Repository structure

- Root command is assembled in `root.go`.
- Each CLI subcommand lives under `cmd/<name>/` and exports a `Command` variable of type `*cobra.Command`.
- `pkg/cli` initializes the root command and registers subcommands.
- `pkg/cmd` holds legacy command registration helpers.
- Core functionality is implemented in `pkg/` packages.
- Tests live under `tests/`, with separate `integration`, `smoke`, and `e2e` suites.

## Important conventions

- Add new CLI commands as a new package under `cmd/<name>`.
- Include a `doc.go` file in command packages with a package-level description.
- Use `pkg/context` for shared application state and configuration.
- `tests/` uses a proxy test binary pattern; prefer `make test-integration`, `make test-smoke`, or `make test-e2e` rather than ad hoc test execution.
- `grew setup --unsafe` is only supported when the binary is built with `devmode` (`make dev`).

## Useful references

- `README.md` — user-facing project overview, install and usage examples
- `docs/tech.md` — architecture, update/self-update, devmode, diagnostics, metadata, and design principles
- `cmd/README.md` — subcommand organization and implementation guidelines
- `tests/README.md` — integration and end-to-end test strategy

## What to avoid

- Do not assume this is a generic Go CLI; it has strong macOS package manager semantics and sandboxing behavior.
- Avoid making changes that bypass the shared `Context` or the package-manager lifecycle without understanding `pkg/context` and `pkg/installer`.
- Do not treat `setup --unsafe` as a normal install path outside developer mode.

## When in doubt

- Prefer linking to existing workspace docs rather than duplicating them.
- Keep changes minimal and focused on the actual command or package being modified.
- Use `make check-all` after edits to verify unit, integration, smoke, and e2e test coverage where appropriate.
