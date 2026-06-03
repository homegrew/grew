# grew copilot instructions

This repository is a macOS-focused Go package manager with a modular Cobra-based CLI.

## Key points

- Build with `make build`; developer mode builds use `make dev`.
- Run tests with `make test-unit`, `make test-integration`, `make test-smoke`, `make test-e2e`, or `make check-all`.
- Each CLI command lives under `cmd/<name>/` and must export a `Command` variable of type `*cobra.Command`.
- Command packages should include `doc.go` with a package-level description.
- `root.go` assembles the CLI and imports command packages via `pkg/cli` and `pkg/cmd`.
- Shared app state is managed by `pkg/context`; use it instead of global variables when adding new commands.
- `tests/README.md` explains the proxy test binary strategy used by integration and smoke tests.

## References

- `AGENTS.md`
- `README.md`
- `docs/tech.md`
- `cmd/README.md`
- `tests/README.md`