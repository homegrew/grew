package cmd

import "fmt"

var commandHelp = map[string]string{
	"install": `Usage: grew install [--cask] [-s] [--only-dependencies] [--ignore-dependencies] <formula>

Install a formula and its dependencies. Downloads the package, verifies
its SHA256 checksum, extracts it to the Cellar, and creates symlinks.

Flags:
  --cask                Install a macOS application cask instead of a formula.
                        Casks are .app bundles installed to ~/Applications.
  -s, --build-from-source
                        Build the formula from source instead of using the
                        pre-built bottle. Downloads the source tarball and
                        runs ./configure && make && make install. Dependencies
                        are still installed from bottles.
  --only-dependencies   Install the dependencies but not the formula itself.
  --ignore-dependencies Skip installing dependencies; install only the formula.
  --skip-post-install   Do not run the post-install script.
  --skip-link           Install to the Cellar but do not create symlinks.
  --require-sha         Refuse to install if a formula is missing a SHA256
                        checksum. Checks all formulas (including dependencies)
                        before downloading anything.

If the formula/cask is already installed, the command is a no-op.

Examples:
  grew install jq
  grew install -s ldns
  grew install --only-dependencies ldns
  grew install --ignore-dependencies jq
  grew install --cask firefox
  grew install --cask visual-studio-code`,

	"uninstall": `Usage: grew uninstall [--cask] <formula>

Uninstall a formula by removing its symlinks and Cellar directory.
With --cask, removes the .app from ~/Applications and the Caskroom entry.

Aliases: remove

Examples:
  grew uninstall jq
  grew uninstall --cask firefox`,

	"list": `Usage: grew list [--cask]

List all installed formulas with their versions.
With --cask, list installed casks instead.

Examples:
  grew list
  grew list --cask`,

	"info": `Usage: grew info [--cask] <formula>

Show detailed information about a formula including its name, version,
description, homepage, license, installed status, dependencies, and
supported platforms. With --cask, show cask details including app artifacts.

Examples:
  grew info jq
  grew info --cask firefox`,

	"search": `Usage: grew search [--cask] <query>

Search available formulas by name or description (case-insensitive
substring match). Installed formulas are marked with *.
With --cask, search casks instead of formulas.

Examples:
  grew search json
  grew search --cask browser`,

	"link": `Usage: grew link [--overwrite] [--dry-run] [--force] <formula>

Create symlinks for an installed formula. Symlinks binaries into bin/,
libraries into lib/, and headers into include/. Also creates an opt/
symlink pointing to the Cellar keg.

For keg-only formulas, only the opt/ symlink is created unless --force
is used.

Flags:
  --overwrite    Overwrite existing files or symlinks from other formulas
  -n, --dry-run  Show what would be linked without making changes
  --force        Link a keg-only formula into bin/, lib/, include/

Examples:
  grew link jq
  grew link --dry-run jq
  grew link --overwrite jq
  grew link --force openssl`,

	"reinstall": `Usage: grew reinstall <formula>

Uninstall and then reinstall a formula. This is useful when an
installation is corrupted or you want a clean slate. The formula
must already be installed.

Examples:
  grew reinstall jq`,

	"unlink": `Usage: grew unlink [--dry-run] <formula>

Remove symlinks for an installed formula without uninstalling it.

Flags:
  -n, --dry-run  Show what would be unlinked without making changes

Examples:
  grew unlink jq`,

	"reset-update": `Usage: grew reset-update

Delete all tap definitions and re-fetch them from scratch. Use this when
'grew update' fails or tap data is corrupted.

What it does:
  1. Removes the entire Taps directory
  2. Re-creates the directory structure
  3. Fetches fresh tap definitions (via API or git clone)

Installed packages in the Cellar are NOT affected.

Examples:
  grew reset-update`,

	"update": `Usage: grew update

Fetch the newest version of grew and all formulae from GitHub
using git(1). Equivalent to: git -C <taps-dir> pull

The taps repository is cloned from:
  https://github.com/homegrew/homegrew-taps`,

	"upgrade": `Usage: grew upgrade [formula ...]

Upgrade outdated formulas to the latest version available in the tap.
With no arguments, upgrades all outdated packages. Specify formula
names to upgrade only those.

The old version keg is removed after a successful upgrade.

Examples:
  grew upgrade
  grew upgrade jq`,

	"outdated": `Usage: grew outdated

List installed formulas that have a newer version available in the tap.`,

	"cleanup": `Usage: grew cleanup [-n] [formula ...]

Remove old versions of installed formulas and clear the download cache.
Only the latest version of each formula is kept.

Flags:
  -n, --dry-run    Show what would be removed without deleting

Examples:
  grew cleanup
  grew cleanup -n
  grew cleanup jq`,

	"alias": `Usage: grew alias <subcommand> [arguments]

Manage command aliases. Aliases let you create shortcuts for
frequently used commands.

Subcommands:
  list, ls             List all aliases (default)
  add <name> <command> Create or overwrite an alias
  rm <name>            Remove an alias
  show <name>          Show what an alias expands to
  edit                 Open the alias file in $EDITOR

Examples:
  grew alias add i install
  grew alias add ri reinstall
  grew alias ls
  grew alias rm i
  grew alias show i
  grew alias edit`,

	"audit": `Usage: grew audit [--strict] [--cask] [--online] [formula ...]

Audit formula or cask definitions for common problems and style issues.
With no arguments, audits all formulas in the core tap.

Checks performed:
  - Missing metadata (description, homepage, license)
  - Homepage uses HTTPS and is a valid URL
  - Name follows conventions (lowercase, valid characters)
  - Version uses valid characters
  - All download URLs use HTTPS and are parseable
  - All SHA256 hashes are valid 64-character hex strings
  - Dependencies exist in the tap and have valid names
  - No circular dependencies
  - No self-dependencies
  - Install type is valid (binary or archive)
  - Binary installs have binary_name set
  - Cask artifacts are correctly defined

Flags:
  --strict    Treat warnings as errors (exit non-zero on warnings)
  --cask      Audit cask definitions instead of formulas
  --online    Include checks that require installed packages
              (verifies snapshot integrity for installed formulas)

Exit code 0 if audit passes, 1 if errors are found (or warnings
with --strict).

Examples:
  grew audit
  grew audit jq
  grew audit --strict
  grew audit --cask
  grew audit --cask firefox
  grew audit --online jq`,

	"deps": `Usage: grew deps [--tree] [--all | --installed] <formula ...>

Show dependencies for one or more formulas. By default shows all
transitive dependencies. Use --tree for a visual tree view.

Flags:
  --tree        Show dependencies as a tree
  --all         Show dependencies for all available formulas
  --installed   Show dependencies for all installed formulas

Examples:
  grew deps jq
  grew deps --tree jq
  grew deps --all
  grew deps --installed`,

	"doctor": `Usage: grew doctor [flags] [check ...]

Check your system for potential problems. Exits with non-zero status
if warnings are found.

Aliases: dr

Flags:
  -a, --all          Run all checks (overrides individual selections)
  --list-checks      List all available check names
  -D, --audit-debug  Show timing and warning count per check
  -q, --quiet        Suppress banner; only print warnings

Security checks:
  check_directory_permissions   World-writable grew directories
  check_formula_https           Formula URLs not using HTTPS
  check_formula_sha256          Invalid or malformed SHA256 hashes
  check_symlink_targets         Symlinks escaping the grew prefix
  check_cellar_permissions      World-writable installed kegs/binaries
  check_snapshot_integrity      Verify packages against install manifests

Structural checks:
  check_directories             Required directories exist
  check_path                    grew bin/ in PATH
  check_core_tap                Core tap has formulas
  check_broken_symlinks         Broken symlinks in bin/, lib/, include/
  check_broken_opt_symlinks     Broken opt/ symlinks
  check_unlinked_kegs           Installed but not linked formulas
  check_orphaned_symlinks       Symlinks to uninstalled formulas
  check_multiple_versions       Multiple versions (suggest cleanup)
  check_stale_tmp               Leftover files in tmp/

Run specific checks by name:
  grew doctor check_formula_https check_directory_permissions

Examples:
  grew doctor
  grew doctor --list-checks
  grew doctor -D
  grew doctor -q
  grew doctor check_symlink_targets`,

	"config": `Usage: grew config

Show grew and system configuration including paths, installed
package count, Go version, OS, CPU, and detected tools (git, curl,
clang). Also shows any HOMEGREW_* environment variables.`,

	"shellenv": `Usage: grew shellenv [shell]

Print export statements for setting up grew in your shell. Add the
output to your shell profile to make grew-installed tools available.

Detects the current shell automatically, or specify one explicitly.
Supported shells: bash, zsh, fish, sh.

Setup:
  # bash (~/.bashrc):
  eval "$(grew shellenv)"

  # zsh (~/.zshrc):
  eval "$(grew shellenv)"

  # fish (~/.config/fish/config.fish):
  grew shellenv fish | source

Examples:
  grew shellenv
  grew shellenv fish`,

	"setup": `Usage: grew setup [--force]

Set up the grew directory structure. Behavior depends on whether you
run it with or without sudo:

  grew setup        → installs to ~/.grew (user-local, no root needed)
  sudo grew setup   → installs to the system prefix (better isolation)

System prefix locations:
  macOS (Apple Silicon): /opt/grew
  macOS (Intel):         /usr/local/grew
  Linux:                 /usr/local/grew

With sudo, the command:
  1. Creates the system prefix directory
  2. Transfers ownership to SUDO_USER (no root needed at runtime)
  3. Creates the internal directory structure
  4. Copies the grew binary into <prefix>/bin/

Without sudo, the command:
  1. Creates ~/.grew and the internal directory structure
  2. Copies the grew binary into ~/.grew/bin/

Path inference: grew infers its prefix from the binary location. If
the binary is at <prefix>/bin/grew, all paths are derived from <prefix>
automatically — no HOMEGREW_PREFIX env var needed.

Security: a system prefix isolates builds from $HOME, preventing
sandboxed formulas from accessing ~/.ssh, ~/.gnupg, etc.

Flags:
  -f, --force   Re-run setup even if already set up

After setup, add to your shell profile:
  eval "$(grew shellenv)"`,

	"services": `Usage: grew services <subcommand> [arguments]

Manage background services for installed formulas. Services are
registered with the platform init system (launchd on macOS,
systemd --user on Linux) so they persist across reboots.

Subcommands:
  list, ls              List all managed services and their status
  start <formula>       Write a service definition and start it
  stop <formula>        Stop the service and remove its definition
  restart <formula>     Stop then start the service
  run <formula>         Run the service command in the foreground
  info <formula>        Show service configuration and status

The service definition comes from the formula's "service" field.
The run command supports {prefix}, {opt}, and {cellar} placeholders
that are expanded to the grew directory paths.

On macOS, services are managed via launchctl (~/Library/LaunchAgents).
On Linux, services are managed via systemd --user (~/.config/systemd/user).

Examples:
  grew services list
  grew services start postgresql
  grew services stop redis
  grew services restart postgresql
  grew services run postgresql
  grew services info postgresql`,

	"verify": `Usage: grew verify [--json] [formula ...]

Verify the integrity of installed packages by comparing the filesystem
against the snapshot manifest recorded at install time.

With no arguments, verifies all installed packages.

Flags:
  --json    Output results as JSON for machine consumption

Each package is checked for:
  - Missing files (deleted after install)
  - Modified files (content changed since install)
  - Added files (unexpected files appeared in the keg)

Exit code 0 if all packages pass, 1 if any discrepancies found.

Examples:
  grew verify
  grew verify jq
  grew verify --json`,

	"help": `Usage: grew help [command]

Show help for grew or a specific command.

Examples:
  grew help
  grew help install`,
}

func runHelp(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	name := args[0]
	// Resolve aliases
	if name == "remove" {
		name = "uninstall"
	}

	text, ok := commandHelp[name]
	if !ok {
		return fmt.Errorf("unknown command: %s\nRun 'grew help' for usage", name)
	}

	fmt.Println(text)
	return nil
}
