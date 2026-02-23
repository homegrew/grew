<!-- Inspired by the Best-README-Template by othneildrew -->

[![Go Version][go-badge]][go-url]

# grew

Lean, fast package manager in Go. CLI name: `gobrew`.

## Table of Contents
- [About](#about)
- [Built With](#built-with)
- [Getting Started](#getting-started)
- [Usage](#usage)
- [Command Overview](#command-overview)
- [Configuration](#configuration)
- [Development](#development)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)
- [Contact](#contact)
- [Acknowledgments](#acknowledgments)

## About
Grew keeps the package manager surface tight while covering the essentials: installs, upgrades, dependency graphs, and a doctor that calls out security and structural issues. The focus is predictable installs, deterministic linking, and clear output.

**Key features**
- Formula and macOS cask installs with SHA256 verification
- Deterministic linking with opt symlinks and dry-run safeguards
- Dependency resolver with optional tree view
- Doctor that flags perms, HTTPS, broken links, and stale kegs
- Alias and shellenv helpers for fast workflows

## Built With
- Go 1.26

## Getting Started
**Prerequisites**
- Go 1.26 or newer
- macOS or Linux shell with `git` and `curl`

**Install**
```bash
git clone https://github.com/homegrew/grew.git
cd grew
go build -o gobrew .
./gobrew help
```

## Usage
```bash
./gobrew install jq
./gobrew install --cask firefox
./gobrew link jq
./gobrew deps --tree jq
./gobrew upgrade
./gobrew cleanup -n
```

## Command Overview
| Command | What it does |
| --- | --- |
| `install` | Install a formula or cask |
| `uninstall` | Remove a formula or cask |
| `list` | List installed packages |
| `info` | Show package details |
| `search` | Search available packages |
| `link` | Create symlinks for a formula |
| `unlink` | Remove symlinks for a formula |
| `update` | Refresh embedded tap definitions |
| `upgrade` | Upgrade outdated packages |
| `outdated` | List packages with newer versions |
| `cleanup` | Remove old versions and cache |
| `deps` | Show dependencies (optionally as a tree) |
| `alias` | Manage command aliases |
| `doctor` | Check for common problems |
| `config` | Show configuration and detected tools |
| `shellenv` | Print shell setup exports |
| `help` | Show help for a command |

## Configuration
Grew stores everything under a single prefix. Override via environment variables.
- `GOBREW_PREFIX` root prefix, defaults to `~/.gobrew`
- `GOBREW_APPDIR` cask install location, defaults to `~/Applications`

Derived paths:
- `GOBREW_CELLAR` `GOBREW_PREFIX/Cellar`
- `GOBREW_TAPS` `GOBREW_PREFIX/Taps`
- `GOBREW_BIN` `GOBREW_PREFIX/bin`
- `GOBREW_TMP` `GOBREW_PREFIX/tmp`

Shell setup:
```bash
# bash (~/.bashrc)
eval "$(./gobrew shellenv)"

# zsh (~/.zshrc)
eval "$(./gobrew shellenv)"

# fish (~/.config/fish/config.fish)
./gobrew shellenv fish | source
```

## Development
```bash
go test ./...
```

Project layout:
- `cmd/` CLI commands
- `pkg/` core packages (cellar, formula, tap, linker, downloader, deps)
- `taps/` embedded formulas and casks

## Roadmap
- Track issues and feature requests: https://github.com/homegrew/grew/issues
- Common asks: better taps sync, richer doctor checks, Windows support

## Contributing
- Fork the project
- Create your feature branch `git checkout -b feature/your-feature`
- Commit changes `git commit -m "Add feature"`
- Push to your fork `git push origin feature/your-feature`
- Open a pull request

## License
No license file yet. Add `LICENSE` to clarify usage and redistribution.

## Contact
- Open an issue: https://github.com/homegrew/grew/issues
- Project link: https://github.com/homegrew/grew

## Acknowledgments
- Best README Template by othneildrew

[go-badge]: https://img.shields.io/badge/go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white
[go-url]: https://go.dev
