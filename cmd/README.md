# grew commands

This directory contains the implementation of individual `grew` subcommands. Each subcommand is isolated in its own package to ensure modularity, ease of testing, and clear dependency management.

## Architecture

The `grew` CLI follows a modular architecture:

1.  **Package root (`cmd/`)**: This directory serves as the container for all subcommands.
2.  **Subcommand Packages (`cmd/<name>/`)**: Each subcommand resides in its own package (e.g., `cmd/install`, `cmd/upgrade`).
3.  **Command Variable**: Every subcommand package exports a consistent `Command` variable of type `*cobra.Command`.
4.  **Registration**: Subcommands are imported and registered into the primary `Grew` root command in `root.go` (located in the repository root).

## Guidelines for Adding New Commands

When adding a new subcommand:

1.  Create a new directory under `cmd/` with the name of the command.
2.  Implement the command in a `.go` file within that directory, using the directory name as the package name.
3.  Export the cobra command as `Command`.
4.  Add a `doc.go` file with a package-level comment describing the command.
5.  Import the new package in `root.go` and add `Grew.AddCommand(<package>.Command)`.
6.  Centralize any reusable logic in `pkg/cmd/` or other appropriate `pkg/` packages.

## List of Commands

| Package | Description |
|:---|:---|
| [alias](./alias) | Name things your way |
| [audit](./audit) | Lint formula/cask definitions for quality and security |
| [autoremove](./autoremove) | Uninstall formulae that were only installed as a dependency |
| [cache](./cache) | Display grew's download cache |
| [cleanup](./cleanup) | Remove old versions and prune download cache |
| [completion](./completion) | Generate shell completion (bash, zsh, fish) |
| [config](./config) | What grew thinks it knows |
| [create](./create) | Create a new formula from a URL |
| [deps](./deps) | Dependency spelunking |
| [doctor](./doctor) | It's not a bug, it's a misconfiguration |
| [extract](./extract) | Internal hidden extraction command |
| [homepage](./homepage) | Open a formula's homepage in a browser |
| [info](./info) | Stalk packages |
| [install](./install) | Install formulas or casks |
| [leaves](./leaves) | List installed formulas that are not dependencies |
| [link](./link) | Weave formulas into your PATH |
| [linkage](./linkage) | Dynamic library linkage analysis |
| [list](./list) | See what you've collected |
| [lock](./lock) | Generate, check, or show a reproducible lockfile |
| [missing](./missing) | Check kegs and casks for missing dependencies |
| [pin](./pin) | Freeze formulas to prevent upgrades |
| [reinstall](./reinstall) | Uninstall + install from scratch |
| [resetupdate](./resetupdate) | Reset the update state |
| [search](./search) | Find the thing |
| [services](./services) | Manage background services |
| [setup](./setup) | One-time prefix setup |
| [shellenv](./shellenv) | Wire up your shell |
| [sign](./sign) | Sign formula SHA256 hashes |
| [tap](./tap) | Tap repo management |
| [uninstall](./uninstall) | Send formulas or casks to the void |
| [unlink](./unlink) | Cut the thread |
| [unpin](./unpin) | Unfreeze formulas to allow upgrades |
| [untap](./untap) | Remove a tapped repository |
| [update](./update) | Refresh tap definitions |
| [upgrade](./upgrade) | Get the new hotness |
| [uses](./uses) | Show which installed formulae depend on a formula |
| [verify](./verify) | Check installed packages against their snapshot manifests |
| [version](./version) | Print version and exit |
| [vulnscan](./vulnscan) | Scan installed packages for security vulnerabilities |
