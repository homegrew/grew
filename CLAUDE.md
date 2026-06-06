# grew — Claude Context File

> This file is the authoritative AI agent briefing for Claude working on the `grew` codebase.
> It consolidates architecture, conventions, security rules, and development workflows.

---

## What is `grew`?

`grew` is a **Go-based, macOS-focused package manager** — a modern, hardened alternative to Homebrew.
It is written entirely in Go (module: `github.com/homegrew/grew`, requires Go 1.26), uses
[Cobra](https://github.com/spf13/cobra) for CLI, and targets Apple Silicon and Intel Macs.

Its design philosophy is: *determinism*, *security*, and *simplicity*.
It is **not** a general Unix tool — macOS-specific behaviour (Seatbelt, `install_name_tool`,
Gatekeeper quarantine, `bspatch`) is intentional and expected.

---

## Repository Layout

```
grew/
├── main.go                  # Entry point — sets version, delegates to Grew (cobra root)
├── root.go                  # Defines root cobra.Command, wires cli.AddCommands + pkg/cmd legacy
├── go.mod / go.sum          # Module: github.com/homegrew/grew, Go 1.26
├── Makefile                 # Dev workflow: build, dev, test-*, fmt, lint, check-all
├── .goreleaser.yaml         # Release pipeline config
├── AGENTS.md                # AI agent guide (canonical source of dev rules)
├── cmd-creation-skill.md    # Skill doc for adding new subcommands
├── cmd/                     # One package per CLI subcommand
│   ├── install/
│   ├── uninstall/
│   ├── upgrade/
│   ├── update/
│   ├── search/
│   ├── info/
│   ├── doctor/
│   ├── tap/ untap/
│   ├── link/ unlink/
│   ├── list/ leaves/
│   ├── deps/
│   ├── lock/ pin/ unpin/
│   ├── cleanup/ autoremove/
│   ├── cache/
│   ├── services/
│   ├── shellenv/ alias/
│   ├── sign/ verify/ audit/
│   ├── vulnscan/
│   ├── create/ homepage/
│   ├── config/ setup/
│   ├── version/ completion/
│   └── ...
├── pkg/                     # Reusable business logic — one concern per package
│   ├── context/             # ★ Central shared state (Context, InstallContext)
│   ├── formula/             # Formula struct + YAML loader
│   ├── cask/                # Cask struct, loader, Caskroom
│   ├── cellar/              # Keg management inside Cellar
│   ├── installer/           # Formula + cask install lifecycle
│   ├── linker/              # Deterministic symlink management
│   ├── downloader/          # HTTP download with SHA256/512 verification
│   ├── depgraph/            # Kahn's topological dep resolver + tree view
│   ├── tap/                 # Tap clone/update/verify (git + signed commit check)
│   ├── signing/             # Ed25519 bottle signing & verification
│   ├── sandbox/             # macOS Seatbelt sandboxing for source builds
│   ├── safepath/            # Path validation; Zip Slip protection
│   ├── relocation/          # Keg relocation via install_name_tool
│   ├── snapshot/            # Per-file SHA256 manifest (.MANIFEST.json)
│   ├── receipt/             # INSTALL_RECEIPT.json provenance metadata
│   ├── lockfile/            # Lockfile: pin exact versions + dep trees
│   ├── config/              # Paths struct (Prefix, Cellar, Taps, Cache, …)
│   ├── cache/               # Download cache management
│   ├── doctor/              # System health checks
│   ├── osvdev/              # OSV.dev CVE vulnerability query
│   ├── quarantine/          # macOS com.apple.quarantine xattr
│   ├── auditlog/            # Structured audit log of install/uninstall events
│   ├── bpatch/              # Multi-hop bspatch binary delta update logic
│   ├── release/             # GitHub release asset discovery
│   ├── homebrew/            # Homebrew API fallback (formulae.brew.sh)
│   ├── ui/                  # ANSI-coloured output helpers (TTY-aware)
│   ├── logger/              # log/slog wrapper (DEBUG/INFO/WARN/ERROR, -v/-d/-q)
│   ├── flags/               # Shared cobra flag definitions
│   ├── fsutil/              # Filesystem utilities
│   ├── runtime/             # Platform/arch detection
│   ├── sudo/                # Privilege escalation (rootless-first)
│   ├── service/             # launchd service management
│   ├── validation/          # Input validation helpers
│   ├── linkage/             # dylib linkage inspection
│   └── cli/                 # cobra wiring: AddCommands, InitializeRootCommand
├── docs/
│   ├── tech.md              # ★ Deep architecture: bootstrapping, self-update, sandboxing
│   ├── comparison.md        # grew vs brew feature comparison table
│   └── ROADMAP.md           # Planned features (bundle, edit, bump-formula-pr, …)
├── tests/
│   ├── unit/
│   ├── integration/
│   ├── smoke/
│   └── e2e/
└── tools/                   # Internal CI tooling (e.g., patcher for release assets)
```

---

## Architecture: Core Concepts

### 1. Central Context (`pkg/context`)

**The single source of truth for all runtime state.** Every command must obtain a `*context.Context`
(or `*context.InstallContext`) and read paths, loaders, and services through it.
**Never use global variables. Never hardcode paths.**

```go
type Context struct {
    Paths      config.Paths    // All major directories (Prefix, Cellar, Taps, Cache…)
    Loader     *formula.Loader // Formula loader (local tap → Homebrew API fallback)
    CaskLoader *cask.Loader    // Cask loader (same fallback pattern)
    Cellar     *cellar.Cellar  // Installed keg management
    Caskroom   *cask.Caskroom  // Installed cask records
}
```

`InstallContext` extends `Context` with a `Linker`, `Downloader`, `AuditLog`, and `GlobalLock`
for the install/reinstall/upgrade lifecycle.

### 2. CLI Command Structure

Each subcommand lives in **`cmd/<name>/`** as its own Go package:
- Must export `var Command *cobra.Command`
- Must include a `doc.go` with a package-level comment
- CLI layer stays **thin** — business logic goes in `pkg/`
- Registered via `pkg/cli.AddCommands()` or `pkg/cmd.AddLegacyCommands()`

### 3. Formula & Cask Loading

`Context.LoadFormula(name)` follows this priority chain:
1. Local tap files under `<Taps>/`
2. Auto-tap if name is fully qualified (`user/repo/formula`)
3. Homebrew API fallback (`formulae.brew.sh`)

Same pattern applies to `Context.LoadCask(name)`.

### 4. Security Model (Critical — Do Not Bypass)

| Layer | Mechanism | Package |
|---|---|---|
| Download integrity | SHA256 + SHA512 dual-hash | `pkg/downloader` |
| Bottle authenticity | Ed25519 signature verification | `pkg/signing` |
| Tap commit trust | Signed git commit verification (`HOMEGREW_TAP_VERIFY`) | `pkg/tap` |
| Source builds | macOS Seatbelt sandbox | `pkg/sandbox` |
| Post-install scripts | Sandboxed execution (keg read-only, no network) | `pkg/sandbox` |
| Archive extraction | Zip Slip path validation | `pkg/safepath` |
| Downloaded apps | macOS Quarantine xattr | `pkg/quarantine` |
| Install integrity | Per-file SHA256 manifest | `pkg/snapshot` |
| Vulnerability guard | OSV.dev query before self-update | `pkg/osvdev` |
| External commands | `--` end-of-options on all exec calls | All packages |

**All external commands must use `--` end-of-options separators** to prevent shell injection.
Shell-free subprocess execution with positional parameters is required throughout.

### 5. Self-Update Mechanism (`grew selfupdate`)

Two strategies, selected automatically:

**Source-based** (if `<prefix>/Grew` git repo exists):
1. `git fetch` → optional signed-commit verify → `git reset --hard origin/main` → `go build`

**Release-based** (binary-only installs):
1. Query GitHub API for latest release
2. **OSV.dev guard**: abort if known CVEs found in target version (fail-closed)
3. **Multi-hop BFS patching**: find shortest patch sequence via `bspatch` to current version
4. Each patch: download → dual-hash verify → apply sequentially
5. **Full download fallback** if `bspatch` unavailable or patching fails
6. **Pre-replacement sandbox health check**: run new binary with `vuln-scan --offline` in restricted sandbox before atomic swap

### 6. Dependency Resolution

`pkg/depgraph` implements **Kahn's algorithm** (topological sort) to resolve and order
dependencies before installation. Supports optional tree-view output.

---

## Developer Workflow (Golden Path)

```bash
make dev            # Build with developer mode flags (enables --unsafe features)
make build          # Production release build

make test-unit          # Fast unit tests
make test-integration   # Integration tests
make test-smoke         # Smoke tests
make test-e2e           # Full end-to-end lifecycle tests (use for final validation)
make check-all          # Run ALL checks — required before submitting changes

make fmt            # Format code (gofmt)
make lint           # Lint (golangci-lint or similar)

./grew doctor       # Verify structural integrity + security compliance after changes
```

---

## Adding a New Command (Checklist)

1. Create `cmd/<name>/` directory
2. Add `doc.go` with package-level comment describing the command
3. Implement `var Command *cobra.Command` in `<name>.go` (or multiple files)
4. Keep the CLI layer thin — delegate all logic to `pkg/` packages
5. Use `pkg/context.New()` for shared state; never use globals
6. Register in `pkg/cli/cli.go` via `AddCommands()`
7. Write tests: unit in `pkg/`, integration/smoke/e2e in `tests/`
8. Run `make check-all` and `grew doctor`

Reference: `cmd-creation-skill.md` in repo root.

---

## Key Environment Variables

| Variable | Purpose |
|---|---|
| `HOMEGREW_NO_INIT_TAP` | Skip core tap initialisation (useful in tests) |
| `HOMEGREW_TAP_VERIFY` | Enable signed git commit verification on taps |
| `HOMEGREW_API_DOMAIN` | Override remote metadata API endpoint (planned) |

---

## Paths (via `pkg/config`)

| Name | Apple Silicon | Intel Mac |
|---|---|---|
| Prefix | `/opt/homegrew` | `/usr/local/homegrew` |
| Cellar | `<prefix>/Cellar` | same |
| Caskroom | `<prefix>/Caskroom` | same |
| Taps | `<prefix>/Library/Taps` | same |
| Cache | `<prefix>/Cache` | same |
| Bin | `<prefix>/bin` | same |

Always access paths via `ctx.Paths.*` — never hardcode.

---

## Anti-Patterns (Never Do These)

- ❌ Use global variables instead of `pkg/context`
- ❌ Hardcode paths like `/opt/homegrew` instead of `ctx.Paths.Cellar`
- ❌ Skip security checks in the self-update path
- ❌ Run external commands without `--` end-of-options separator
- ❌ Use `sudo` outside of initial prefix setup (`pkg/sudo`)
- ❌ Duplicate large documentation blocks — link to `docs/tech.md` instead
- ❌ Add macOS-specific behaviour without platform guards
- ❌ Bypass the installer lifecycle (`pkg/installer`) with direct file operations

---

## Roadmap Highlights (from `docs/ROADMAP.md`)

- `grew bundle` — declarative `Grewfile` support (dump + install)
- `grew edit <formula>` — open formula YAML in `$EDITOR`
- `grew bump-formula-pr` — automated version bump + PR submission
- `grew zap <cask>` — deep uninstall including config/cache artefacts
- `.pkg` installer support for casks
- `grew uses <formula>` — reverse dependency lookup
- `grew missing` — detect broken dependency chains
- Auto-update cooldown (24h tap freshness check before install/upgrade)

---

## References

- `AGENTS.md` — canonical AI agent contribution guide
- `docs/tech.md` — deep dive: bootstrapping, self-update, sandboxing internals
- `docs/comparison.md` — grew vs brew feature parity table
- `cmd-creation-skill.md` — step-by-step new command guide
