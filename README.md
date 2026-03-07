# 🥤 grew

> *A lean, mean, package-managing machine. In Go.*

[![Go Version][go-badge]][go-url]

`grew` is what happens when you look at your package manager and think: *"This could be so much simpler."* Deterministic installs. Clean symlinks. A doctor that actually tells you what's wrong. No drama.

> 💬 **A word from the author:**
> *I've been a die-hard Homebrew user for longer than I care to admit. brew and I? We go way back. Late nights, broken PATH, the works — and I loved every minute of it. I love brew so much, in fact, that I thought: "What if I just… made it better?" Audacious? Absolutely. Foolish? Possibly. Fun? You bet. grew is my love letter to brew — written in Go, with a cheeky grin.*

---

## ✨ What it does

- 📦 **Formula + cask installs** with SHA256 verification (no funny business)
- 🔒 **Sandboxed source builds** using macOS Seatbelt or Linux namespaces to keep your system safe
- 🔐 **Sandboxed post-install scripts** — keg is read-only, network denied, minimal env (Homebrew runs these unsandboxed)
- ✍️ **Ed25519 bottle signing** — cryptographic signatures on downloads, verified against a local trust store
- 🏷️ **Signed tap verification** — refuse or warn on unsigned git commits in tap repos (`HOMEGREW_TAP_VERIFY`)
- 📋 **Install snapshots** — per-file SHA256 manifests recorded at install time for integrity verification
- 📌 **Lockfile** — pin exact versions, hashes, and dependency trees for reproducible environments
- 🔗 **Deterministic linking** with opt symlinks and dry-run support (look before you link)
- 🌳 **Dependency resolver** with an optional tree view (for the visually inclined)
- 🩺 **Doctor** that checks perms, HTTPS, broken links, snapshot integrity, and stale kegs
- 🐚 **Alias + shellenv helpers** so your workflows stay snappy

---

## 🚀 Getting Started

**Prerequisites:** Go 1.26+, `git`, and a dream.

### Build from source

```bash
git clone https://github.com/homegrew/grew.git
cd grew
make build          # or: go generate ./internal/... && go build -o grew
```

### Set up the prefix

grew needs a home — a directory tree for the Cellar, symlinks, taps, and config. The `setup` command creates it and copies the binary into place:

```bash
# User-local install (no root needed) — installs to ~/.grew
./grew setup

# System install (recommended — isolates builds from $HOME)
sudo ./grew setup   # macOS ARM → /opt/grew, Intel/Linux → /usr/local/grew
```

The system prefix is more secure: sandboxed source builds can't reach `~/.ssh`, `~/.gnupg`, or other sensitive dotfiles.

### Wire up your shell

Add this to your shell profile so grew-installed binaries are on your PATH:

```bash
# bash (~/.bashrc) or zsh (~/.zshrc)
eval "$(grew shellenv)"

# fish (~/.config/fish/config.fish)
grew shellenv fish | source
```

### Install something

```bash
grew install jq
grew install --cask firefox
```

That's it. No dark rituals. No 47-step setup guide.

---

## 📖 Usage

```bash
./grew install jq              # the classic
./grew install -s ldns         # build from source, like a purist
./grew install --cask firefox  # going big
./grew link jq                 # stitch it in
./grew deps --tree jq          # what hath jq wrought
./grew upgrade                 # stay fresh
./grew cleanup -n              # peek before you sweep
./grew verify jq               # check installed files against manifest
./grew lock                    # pin your environment
./grew audit --strict          # lint your formulas
```

---

## 🗺️ Commands

| Command | What it does |
|---|---|
| `install` | Install a formula or cask (`-s` to build from source) |
| `uninstall` | Send it to the void |
| `list` | See what you've collected |
| `info` | Stalk a package |
| `search` | Find the thing |
| `link` | Weave a formula into your PATH |
| `unlink` | Cut the thread |
| `update` | Refresh tap definitions |
| `upgrade` | Get the new hotness |
| `outdated` | The hall of shame |
| `reinstall` | Uninstall + install from scratch |
| `cleanup` | Marie Kondo your Cellar |
| `deps` | Dependency spelunking |
| `alias` | Name things your way |
| `verify` | Check installed packages against their snapshot manifests |
| `audit` | Lint formula/cask definitions for quality and security |
| `lock` | Generate, check, or show a reproducible lockfile |
| `sign` | Sign formula SHA256 hashes with an Ed25519 key |
| `services` | Manage background services (start, stop, restart, list) |
| `setup` | One-time prefix setup (user-local or system-wide with sudo) |
| `doctor` | It's not a bug, it's a misconfiguration |
| `config` | What grew thinks it knows |
| `shellenv` | Wire up your shell |
| `help` | You got this |

---

## ⚙️ Configuration

grew keeps its stuff tidy under one roof. Tweak it with env vars:

| Variable | Default | What it is |
|---|---|---|
| `HOMEGREW_PREFIX` | `~/.grew` | The kingdom |
| `HOMEGREW_APPDIR` | `~/Applications` | Where casks live |
| `HOMEGREW_TAP_VERIFY` | `off` | Tap commit signature policy (`off`, `warn`, `strict`) |
| `HOMEGREW_NO_INSTALL_FROM_API` | *(unset)* | Force git clone instead of API tarball for taps |

Everything else flows from the prefix:

```
~/.grew/
├── Cellar/        ← installed packages (each keg has a .MANIFEST.json)
├── Taps/          ← formula definitions (git-cloned or API-fetched)
├── bin/           ← symlinked binaries
├── etc/           ← trusted-keys (Ed25519 public keys, one per line)
├── tmp/           ← ephemeral stuff
└── grew.lock      ← lockfile (opt-in, created by `grew lock`)
```

---

## 🛠️ Development

```bash
make check         # go test -v -race ./...
make build         # go generate + go build
make lint          # golangci-lint (if installed)
```

**Project layout:**

```
grew/
├── internal/
│   ├── cmd/          ← CLI commands (the face)
│   ├── cellar/       ← installed package management
│   ├── formula/      ← formula parsing and validation
│   ├── cask/         ← cask parsing and Caskroom
│   ├── linker/       ← deterministic symlink management
│   ├── depgraph/     ← dependency resolution (Kahn's toposort)
│   ├── downloader/   ← HTTP download + SHA256 + archive extraction
│   ├── tap/          ← tap repo management + commit verification
│   ├── sandbox/      ← build + post-install sandboxing (macOS/Linux)
│   ├── signing/      ← Ed25519 bottle signing + trust store
│   ├── snapshot/     ← per-file manifest capture + integrity verification
│   ├── lockfile/     ← reproducible environment pinning
│   ├── service/      ← background service management (launchd/systemd)
│   ├── config/       ← prefix + path resolution
│   ├── validation/   ← name/version/SHA256 validation
│   └── version/      ← embedded version from git tags
└── tools/            ← import scripts (Homebrew formula/cask conversion)
```

---

## 🔐 Security Model

grew is designed to be more secure than Homebrew out of the box:

| Feature | grew | Homebrew |
|---|---|---|
| **Bottle signing** | Ed25519 signatures verified against local trust store | None — relies on HTTPS + SHA256 only |
| **Tap verification** | Optional GPG/SSH commit signature enforcement | None |
| **Post-install sandbox** | Read-only keg, no network, minimal env | Unsandboxed |
| **Source build sandbox** | macOS Seatbelt / Linux bwrap+unshare, no network | macOS Seatbelt only, no Linux |
| **Install manifests** | Per-file SHA256 snapshot at install time | None |
| **Lockfile** | Full dependency tree with hashes | None |
| **Integrity check** | `grew verify` + `grew doctor` snapshot check | None |
| **HTTPS enforcement** | At parse time — HTTP URLs rejected before download | At download time |

**Gradual rollout:** signature verification doesn't block installs until you add keys to `etc/trusted-keys`. Tap verification is opt-in via `HOMEGREW_TAP_VERIFY`. This lets you adopt security features incrementally.

---

## 🗺️ Roadmap

Got ideas? Bugs? Grievances? → [Open an issue](https://github.com/homegrew/grew/issues)

Hot takes on the list:
- SLSA provenance attestations for bottles
- Vulnerability scanning (OSV/NVD integration)
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

No license file yet — add a `LICENSE` to clarify what others can and can't do with your code. (It's the responsible thing to do. We believe in you.)

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
