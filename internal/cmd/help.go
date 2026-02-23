package cmd

import "fmt"

var commandHelp = map[string]string{
	"install": `Usage: grew install [--cask] <formula>

Install a formula and its dependencies. Downloads the package, verifies
its SHA256 checksum, extracts it to the Cellar, and creates symlinks.

Flags:
  --cask    Install a macOS application cask instead of a formula.
            Casks are .app bundles installed to ~/Applications.

If the formula/cask is already installed, the command is a no-op.

Examples:
  grew install jq
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

	"update": `Usage: grew update

Update formula definitions by re-syncing the core tap from the
embedded formulas shipped with the grew binary.`,

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
