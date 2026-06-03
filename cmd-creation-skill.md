# Command Creation Skill

This skill helps create new `cmd/<name>` subcommands for the `grew` repository.

## When to use

- Adding a new CLI feature
- Extending `grew` with a new top-level command
- Refactoring a command into its own package

## What to do

- Create a new package directory under `cmd/<name>`
- Implement the command in one or more `.go` files
- Export a `Command` variable of type `*cobra.Command`
- Add a `doc.go` file with a package-level comment describing the command
- Keep command logic minimal in the CLI layer; delegate business logic to `pkg/` packages
- Use `pkg/context` for shared configuration and environment loading
- Register the command in `root.go` via `pkg/cli` or `pkg/cmd` as appropriate
- Update tests if needed, favoring existing patterns in `tests/integration`, `tests/smoke`, or unit tests

## What to avoid

- Adding global state outside `pkg/context`
- Hardcoding macOS-specific behavior without platform guards
- Bypassing the package manager lifecycle or internal installer abstractions