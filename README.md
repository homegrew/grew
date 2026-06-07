# 🥤 grew

> *A lean, mean, package-managing machine. In Go.*

[![Go Version][go-badge]][go-url]

`grew` is what happens when you look at your package manager and think: *"This could be so much simpler."* Deterministic installs. Clean symlinks. A doctor that actually tells you what's wrong. No drama.

> 💬 **A word from the author:**
> *I've been a die-hard Homebrew user for longer than I care to admit. brew and I? We go way back. Late nights, broken PATH, the works — and I loved every minute of it. I love brew so much, in fact, that I thought: "What if I just… made it better?" Audacious? Absolutely. Foolish? Possibly. Fun? You bet. grew is my love letter to brew — written in Go, with a cheeky grin.*

---

## ✨ What it does

- 📦 **Formula + cask installs** with SHA256 verification (no funny business)
- 🚰 **Tap auto-install** — automatically clones missing taps when you request `user/repo/formula`
- ⚡ **Multi-hop binary delta updates** — `selfupdate` uses `bspatch` to seamlessly apply sequences of intermediate patches to reach the latest version, saving bandwidth, with an automated CI `patcher` tool for releases and `-U` upgrade path verification
- 🔐 **Dual-hash verification** — self-updates and release assets are verified against both SHA256 and SHA512 to prevent single-algorithm collision attacks
- 🔒 **Sandboxed source builds** using macOS Seatbelt to keep your system safe
- 🔐 **Sandboxed post-install scripts** — keg is read-only, network denied, minimal env (Homebrew runs these unsandboxed)
- ✍️ **Ed25519 bottle signing** — cryptographic signatures on downloads, verified against a local trust store
- 🏷️ **Signed tap verification** — refuse or warn on unsigned git commits in tap repos (`HOMEGREW_TAP_VERIFY`)
- 📋 **Install snapshots** — per-file SHA256 manifests (`.MANIFEST.json`) recorded at install time for integrity verification
- 🧾 **Installation receipts** — stores build options, dependencies, and provenance metadata (`INSTALL_RECEIPT.json`) in the keg for future reference
- 📌 **Lockfile** — pin exact versions, hashes, and dependency trees for reproducible environments
- 🔗 **Deterministic linking** with opt symlinks and dry-run support (look before you link)
- 🔄 **Keg relocation** — rewrites hardcoded library paths in bottles at install time via `install_name_tool`, so binaries just work without `DYLD_LIBRARY_PATH` hacks
- 🌳 **Dependency resolver** with an optional tree view (for the visually inclined)
- 🩺 **Doctor** that checks perms, HTTPS, broken links, snapshot integrity, stale kegs, and cask notarization
- 🛡️ **Hardened command execution** — `--` end-of-options on all external commands, shell-free namespace setup with positional parameters, XML-safe plist generation
- 🧱 **Zip Slip protection** — archive extraction validates symlink indirection to prevent writes outside the destination
- 🔍 **Vulnerability scanning** — queries OSV.dev for known CVEs via the `vuln-scan` command
- 🛡️ **macOS Quarantine** — automatically applies `com.apple.quarantine` attributes to downloaded apps and binaries, ensuring Gatekeeper protection is active
- 🪵 **Structured logging** via `log/slog` with CLI-friendly output (DEBUG/INFO/WARN/ERROR levels, `-v`/`-d`/`-q` flags). Debug logs include source file and line number context.
- 🎨 **Colorful output** — ANSI-colored output with automatic TTY detection for a polished, Homebrew-like aesthetic.
- 🐚 **Alias + shellenv helpers** so your workflows stay snappy (`i`, `rm`, `ls`, `up`, `ug`, `dr`)

---

## 🚀 Getting Started

### Get Grew

Download the latest release for your platform from the [Releases](https://github.com/homegrew/grew/releases/latest) page, extract it, and run setup:

```bash
tar -xzf grew_*.tar.gz
./grew setup
```

### Build from source

**Prerequisites:** Go 1.26+, `git`, and a dream.

```bash
git clone https://github.com/homegrew/grew.git
cd grew
make build          # or: go generate ./pkg/... && go build -o grew
```

### Set up the prefix

grew needs a home — a directory tree for the Cellar, symlinks, taps, and config. The `setup` command creates it and copies the binary into place:

```bash
./grew setup   # macOS ARM → /opt/homegrew, Intel → /usr/local/homegrew
```

The system prefix isolates sandboxed builds from `$HOME`, preventing them from reaching `~/.ssh`, `~/.gnupg`, or other sensitive dotfiles. After setup, ownership is transferred to your user — no root needed at runtime.

### Wire up your shell

Add this to your shell profile so grew-installed binaries and libraries are available:

```bash
# bash (~/.bashrc) or zsh (~/.zshrc)
eval "$(grew shellenv)"

# fish (~/.config/fish/config.fish)
grew shellenv fish | source
```

### Install something

```bash
grew i jq                   # 'i' is an alias for 'install'
grew install --cask firefox
```

That's it. No dark rituals. No 47-step setup guide.

---

## 🗑️ Uninstallation

`grew` stores most of its data in its prefix directory, but some items (like Cask applications and background services) are linked to system directories. To completely remove `grew` and all of its traces:

**1. Clean up installed packages (Casks and Services):**

Uninstall casks and stop services first so `grew` can clean up `/Applications` and your service managers (launchd/systemd):

```bash
# Stop and remove all background services
for s in $(grew services ls | awk 'NR>1 {print $1}'); do grew services stop $s; done

# Uninstall all macOS casks
for c in $(grew list --cask | awk '{print $1}'); do grew uninstall --cask $c; done
```

**2. Delete the prefix directory:**

*   **macOS (Apple Silicon):**
    ```bash
    sudo rm -rf /opt/homegrew
    ```
*   **macOS (Intel):**
    ```bash
    sudo rm -rf /usr/local/homegrew
    ```
*   **Devmode (User-local install via `--unsafe`):**
    ```bash
    rm -rf ~/.homegrew
    ```

**3. Clean up your shell profile:**

Open your shell configuration file (e.g., `~/.zshrc`, `~/.bashrc`, or `~/.config/fish/config.fish`) and remove the line that initializes `grew`:

```bash
# Remove this line:
eval "$(/opt/homegrew/bin/grew shellenv)"
```

Restart your terminal, and `grew` is completely gone.

---

## 📖 Usage

For an in-depth look at how `grew` installs itself, its self-update mechanism, and the developer mode, check out the [Architecture & Technical Details](docs/tech.md).

```bash
grew i jq nmap               # install multiple formulas (alias 'i')
grew install --force jq      # force reinstall even if already installed
grew install -s ldns         # build from source, like a purist
grew install --force-bottle jq  # pour a bottle (current or newest macOS), never build
grew install --cask firefox  # going big
grew link jq                 # stitch it in
grew deps --tree jq          # what hath jq wrought
grew up                      # stay fresh (alias 'up' for update)
grew ug                      # upgrade all (alias 'ug')
grew version                 # what are we running
grew autoremove --dry-run    # see which orphaned dependencies would be removed
grew autoremove              # clean up unused dependencies
grew rm --force jq           # uninstall even if not installed (alias 'rm')
grew cleanup -n              # peek before you sweep
grew cleanup --scrub         # aggressive cache cleaning
grew cleanup --prune=7       # remove cache older than a week
grew verify jq               # check installed files against manifest
grew vuln-scan -q            # scan for CVEs and only show critical/high severity findings
grew lock                    # pin your environment
grew audit --strict          # lint your formulas
grew cache                   # show download cache
grew cache jq                # show cache path for jq
grew cache --os=darwin jq    # show cache path for a different OS
grew leaves -r | xargs grew uninstall # uninstall all top-level packages installed on request
grew missing                              # check every installed keg for missing runtime deps
grew missing jq                           # check a single formula's dependency chain
grew missing --hide=openssl@3             # check as if openssl@3 were not installed
grew missing --hide=openssl@3,readline    # hide multiple formulae (comma-separated)
```

---

## 🗺️ Commands

| Command | What it does |
|---|---|
| `install, i` | Install formulas or casks (`-f` to force, `-s` to build from source, `--force-bottle` to force a bottle) |
| `uninstall, rm` | Send formulas or casks to the void (`-f` to ignore missing or errors, delete all versions) |
| `autoremove` | Transitively uninstall formulae that were only installed as a dependency and are no longer needed — removes the full orphan chain in one run (`--dry-run` supported) |
| `list, ls` | See what you've collected |
| `leaves [-r] [-p]` | List installed formulas that are not dependencies of another installed formula |
| `info` | Stalk packages |
| `search` | Find the thing |
| `link` | Weave formulas into your PATH |
| `unlink` | Cut the thread |
| `update, up` | Refresh tap definitions |
| `upgrade, ug` | Get the new hotness |
| `outdated` | The hall of shame |
| `reinstall` | Uninstall + install from scratch (`--cask`, `-f` without checking for previously installed keg-only or non-migrated versions) |
| `cleanup` | Remove old versions and prune download cache (`-s` to scrub all, `--prune=DAYS`) |
| `deps` | Dependency spelunking |
| `alias` | Name things your way |
| `audit` | Lint formula/cask definitions for quality and security |
| `create <url>` | Scaffold a new formula from a URL (infers name, version, and SHA256) |
| `homepage <formula>` | Open a formula or cask's homepage in the default browser |
| `uses <formula>` | Show installed formulae that depend on the specified formula |
| `missing [--hide=<list>]` | Check installed kegs for missing runtime dependencies (exits non-zero if any are found) |
| `tap <user/repo>` | Add a formula repository |
| `untap <user/repo>` | Remove a tapped formula repository |
| `linkage <formula>` | Inspect dynamic library dependencies for an installed formula |
| `lock` | Generate, check, or show a reproducible lockfile |
| `verify` | Check installed packages against their snapshot manifests |
| `sign` | Sign formula SHA256 hashes with an Ed25519 key |
| `services` | Manage background services (start, stop, restart, list) |
| `setup` | One-time prefix setup |
| `doctor, dr` | It's not a bug, it's a misconfiguration |
| `vuln-scan` | Scan installed packages for security vulnerabilities |
| `config` | What grew thinks it knows |
| `shellenv` | Wire up your shell |
| `pin` / `unpin` | Freeze formulas to prevent upgrades |
| `completion` | Generate shell completion (bash, zsh, fish) |
| `cache` | Display download cache root or specific package cache paths (alias `--cache`) |
| `version` | Print version and exit |
| `help` | You got this |

---

## ⚙️ Configuration

grew keeps its stuff tidy under one roof. Tweak it with env vars:

| Variable | Default | What it is |
|---|---|---|
| `HOMEGREW_PREFIX` | *(inferred from binary location)* | Root of the grew tree |
| `HOMEGREW_APPDIR` | `/Applications` | Where casks live |
| `HOMEGREW_TAP_VERIFY` | `off` | Tap commit signature policy (`off`, `warn`, `strict`) |
| `HOMEGREW_ALLOWED_HOSTS` | *(built-in allowlist)* | Additional hosts for SSRF-protected downloads |
| `HOMEGREW_CLEANUP_MAX_AGE_DAYS` | `120` | Max age in days for cached downloads |

Everything else flows from the prefix:

```
/opt/homegrew/              (or /usr/local/homegrew on Intel)
├── Cellar/        ← installed packages (each keg has a .MANIFEST.json)
├── Taps/          ← formula definitions (git-cloned or API-fetched)
├── bin/           ← symlinked binaries
├── lib/           ← symlinked libraries
├── include/       ← symlinked headers
├── opt/           ← per-formula keg symlinks
├── etc/           ← trusted-keys (Ed25519 public keys, one per line)
├── tmp/           ← ephemeral stuff
├── var/log/       ← audit log
└── grew.lock      ← lockfile (opt-in, created by `grew lock`)
```

---

## 🛠️ Development

[![Go Reference][pkg-badge]][pkg-url]

```bash
make test-unit     # run unit tests
make test-smoke    # run quick health checks
make test-integration # run command-level integration tests
make test-e2e      # run full lifecycle E2E tests (takes several minutes)
make check-all     # run all of the above
make dev           # build with 'devmode' tag enabled
```

### Developer mode

Release builds will prompt for elevated privileges to setup the system prefix. For local development you can build with the `devmode` tag and pass `--unsafe` to setup to install to `~/.homegrew` without root:

```bash
make dev
./grew setup --unsafe    # installs to ~/.homegrew as your user
./grew install jq        # works without root
```

Both gates are required — the build tag compiles in the code path, and `--unsafe` activates it at setup time. Release binaries ignore `--unsafe` entirely.

### Project layout

```
grew/
├── cmd/              ← standalone command packages (install, upgrade, etc.)
├── pkg/
│   ├── auditlog/     ← persistent record of all install/upgrade/tap actions
│   ├── bpatch/       ← binary delta patching (using bspatch)
│   ├── cache/        ← download cache management and pruning
│   ├── cask/         ← cask parsing, Caskroom, and helpers
│   ├── cellar/       ← installed package management and cleanup
│   ├── cli/          ← shared CLI initialization and command registration
│   ├── cmd/          ← legacy command bridge and high-level orchestration
│   ├── depgraph/     ← dependency resolution (Kahn's toposort)
│   ├── downloader/   ← HTTP download + SHA256/512 + archive extraction
│   ├── flags/        ← global CLI flags (-v, -d, -q)
│   ├── formula/      ← formula parsing and dependency gathering
│   ├── fsutil/       ← atomic file writes, safe tree copies, advisory locking, mode sanitization
│   ├── homebrew/     ← Homebrew JSON API client and compatibility logic
│   ├── installer/    ← core installation logic (formula, cask, self-update)
│   ├── linkage/      ← dynamic library linkage analysis
│   ├── linker/       ← deterministic symlink management
│   ├── lockfile/     ← reproducible environment pinning
│   ├── osvdev/       ← OSV.dev API client for vulnerability scanning
│   ├── quarantine/   ← macOS quarantine attribute and Trash management
│   ├── receipt/      ← installation receipt management
│   ├── release/      ← grew release management and download helpers
│   ├── relocation/   ← keg relocation (rewrite dylib/ELF paths)
│   ├── runtime/      ← runtime environment (root detection, prefix, devmode gate)
│   ├── sandbox/      ← build + post-install sandboxing (macOS Seatbelt)
│   ├── service/      ← background service management
│   ├── signing/      ← Ed25519 bottle signing + trust store
│   ├── sudo/         ← secure privilege escalation handling
│   ├── tap/          ← tap repo management + commit verification
│   ├── config/       ← prefix and path resolution
│   ├── context/      ← unified execution context (Context, InstallContext)
│   ├── doctor/       ← diagnostic engine and checks
│   ├── logger/       ← CLI-friendly log/slog handler with source context
│   ├── safepath/     ← path traversal and Zip Slip protection (SafeJoin, CleanPath, etc.)
│   ├── snapshot/     ← per-file manifest capture + integrity verification
│   ├── validation/   ← name/version/SHA256/path validation
│   └── version/      ← embedded version and helpers
├── tests/
│   ├── integration/  ← command-level integration tests
│   ├── smoke/        ← quick health checks
│   ├── e2e/          ← full lifecycle end-to-end tests
│   ├── testbin/      ← test proxy binary source
│   └── testhelper/   ← shared test utilities
├── root.go           ← Root CLI command definition (Grew)
├── main.go           ← CLI entry point
└── tools/            ← genrepo (converter), patcher (delta patch generator)
```

---

## 🔐 Security Model

grew is designed to be more secure than Homebrew out of the box:

| Feature | grew | Homebrew |
|---|---|---|
| **Bottle signing** | Ed25519 signatures verified against local trust store | None — relies on HTTPS + SHA256 only |
| **Tap verification** | Optional GPG/SSH commit signature enforcement | None |
| **Post-install sandbox** | Read-only keg, no network, minimal env | Unsandboxed |
| **Source build sandbox** | macOS Seatbelt, no network | macOS Seatbelt only |
| **Install manifests** | Per-file SHA256 snapshot (`.MANIFEST.json`) at install time | None |
| **Installation receipts** | Provenance and dependency metadata (`INSTALL_RECEIPT.json`) stored alongside the manifest | Metadata stored in `INSTALL_RECEIPT.json` |
| **Lockfile** | Full dependency tree with hashes | None |
| **Integrity check** | `grew verify` + `grew doctor` snapshot check | None |
| **Dual-hash verification** | Self-updates and release assets use both SHA256 and SHA512 | None |
| **Self-update health check** | Patched binaries are execution-tested in a sandbox before replacement | None |
| **HTTPS enforcement** | At parse time — HTTP URLs rejected before download | At download time |
| **Path traversal protection** | Validated at cellar, linker, loader, and archive extraction layers | Partial |
| **Shell injection prevention** | Namespace setup uses positional parameters to eliminate injection risks; systemd `ExecStart` and launchd plist values properly escaped | N/A |
| **Zip Slip protection** | Symlink indirection attacks blocked during tar/zip extraction | Partial |
| **Command argument hardening** | `--` end-of-options separator on all external commands (`git`, `systemctl`, `launchctl`, `hdiutil`, `tar`, etc.) | Not consistently applied |
| **macOS Quarantine** | `Apply()` via LaunchServices for all downloads | None |
| **Vulnerability scanning** | Integrated `vuln-scan` powered by OSV.dev | Requires external gems/tools |

**Gradual rollout:** signature verification doesn't block installs until you add keys to `etc/trusted-keys`. Tap verification is opt-in via `HOMEGREW_TAP_VERIFY`. This lets you adopt security features incrementally.

---

## 🗺️ Roadmap

Got ideas? Bugs? Grievances? → [Open an issue](https://github.com/homegrew/grew/issues)

Hot takes on the list:
- Build vs Runtime Dependency distinction
- SLSA provenance attestations for bottles
- Content-addressable bottle storage
- Windows support (one day, probably, maybe)

---

## 🤝 Contributing

1. Fork it
2. Branch it (`git checkout -b feature/your-cool-thing`)
3. Commit it (`git commit -m "Add the cool thing"`)
4. Push it (`git push origin feature/your-cool-thing`)
5. PR it

PRs welcome. Drama not so much.

---

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## 📬 Contact

- 🐛 [Open an issue](https://github.com/homegrew/grew/issues)
- 🔗 [Project on GitHub](https://github.com/homegrew/grew)

---

## 💛 Acknowledgments

- [Best-README-Template](https://github.com/othneildrew/Best-README-Template) — the scaffold beneath the scaffold
- Everyone who ever squinted at a wall of package manager output and thought *"there has to be a better way"*

[go-badge]: https://img.shields.io/badge/go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white
[go-url]: https://go.dev
[pkg-badge]: https://pkg.go.dev/badge/github.com/homegrew/grew.svg
[pkg-url]: https://pkg.go.dev/github.com/homegrew/grew
