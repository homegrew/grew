# `grew install` vs `brew install` — Comparison

## What's closely matched

| Feature | brew | grew | Match |
|---|---|---|---|
| **Cellar/keg layout** | `Cellar/<name>/<version>/` | Same | Identical |
| **Opt symlinks** | `opt/<name> → Cellar/…` | Same | Identical |
| **Prefix linking** | bin/, lib/, include/ into prefix | Same | Identical |
| **Shared dir expansion** | Expand symlink-to-dir into per-file symlinks | `unsymDir()` does same | Identical |
| **Keg-only formulas** | Install but don't link into prefix | Same via `f.KegOnly` | Identical |
| **Dependency resolution** | Topological sort (deps first) | Kahn's algorithm in `depgraph/` | Identical concept |
| **Bottle install** | Download prebuilt → verify → extract → link | Same flow exactly | Very close |
| **Source build** | `./configure && make && make install` in sandbox | Same 3-step in `installFormulaFromSource` | Similar |
| **SHA256 verification** | Verify after download | `VerifySHA256()` after download | Identical |
| **Post-install scripts** | Run after linking | Same, sandboxed | Close |
| **`--skip-link` / `--skip-post-install`** | Supported | Supported | Identical |
| **`--only-dependencies` / `--ignore-dependencies`** | Supported | Supported | Identical |
| **`--build-from-source` / `-s`** | Supported | Supported | Identical |
| **`--dry-run` / `-n`** | Supported | `simulateInstall()` | Identical |
| **Pin support** | PINNED marker file | Same | Identical |
| **Cask install** | Separate `--cask` path | Same routing via `caskInstall()` | Same pattern |
| **Multiple installs** | `brew install foo bar` | `grew install foo bar` | Identical |
| **Pour bottle relocation** | Text/binary patching of prefix paths | `internal/relocation` via `install_name_tool`/`patchelf` | Close |
| **`autoremove`** | `brew autoremove` | `grew autoremove` | Identical |
| **JSON output** | `brew info --json` | `grew info --json` | Identical |
| **CLI Framework** | Homebrew-specific Ruby CLI | `github.com/spf13/cobra` with `pkg/ui` | Both feature robust routing and colored output |
| **Caveats** | Formula-specific post-install messages | Supported via `caveats` field | Identical |
| **Automatic cleanup** | Auto-removes old versions after upgrade | Same logic via internal cleanup | Identical |
| **Tap auto-install** | `brew install user/tap/formula` auto-taps | Same auto-tap logic during resolution | Identical |

## Where grew goes further than brew

| Feature | Notes |
|---|---|
| **Ed25519 bottle signing** | Brew relies on GitHub HTTPS trust; grew has a local trust store + per-formula signatures |
| **Install manifests** | `.MANIFEST.json` with per-file SHA256, provenance, aggregate hash — brew has `INSTALL_RECEIPT.json` but it's less comprehensive |
| **Sandboxed extraction** | Grew sandboxes the archive extraction step too, not just builds |
| **Post-install sandbox** | Keg is **read-only** during post-install — brew doesn't enforce this |
| **SSRF host allowlist** | Hardcoded + `HOMEGREW_ALLOWED_HOSTS` — brew doesn't restrict download hosts |
| **Zip Slip + symlink escape protection** | Multi-layer: textual check + `EvalSymlinks` + `withinDir()` — brew relies on system tar |
| **File mode sanitization** | Strips setuid/setgid/sticky/world-write bits on extraction |
| **Audit logging** | Records every install, upgrade, self-update, and tap update action (including failures and skips) with hashes and methods |
| **`--require-sha`** | Refuse install if SHA256 is missing — brew doesn't have this flag |

## Where brew is more capable

| Feature | Notes |
|---|---|
| **Ruby DSL formulas** | Brew formulas are full Ruby classes with `def install` blocks — arbitrary build logic. Grew's source builds are hardcoded to `./configure && make && make install` |
| **Patches** | Brew supports inline/remote patches via `patch do ... end`. Grew has no patching system |
| **Build environment** | Brew sets up `superenv`/`stdenv` with compiler wrappers, rpath fixups, `-isysroot` injection. Grew passes through a clean env but no compiler wrapping |
| **Options/variants** | Brew had `--with-*` / `--without-*` options (deprecated but existed). Grew has none |
| **Tab/receipt metadata** | Brew writes `INSTALL_RECEIPT.json` with build options, compiler info, runtime deps, etc. |
| **Analytics** | Brew reports install analytics (opt-out). Grew has no analytics |
| **HEAD installs** | `brew install --HEAD` builds from repo HEAD. Grew doesn't support this |
| **Build dependencies** | Brew distinguishes `depends_on` vs `build.depends_on`. Grew has flat `dependencies[]` |
| **Bottle auto-selection** | Brew's bottle logic handles OS version matching, fallback bottles, and cellar relocation types. Grew uses simple `os_arch` platform keys |

## Verdict

**~80-85% feature parity** with `brew install` for the core happy path. The fundamental architecture (cellar, kegs, linking, dependency resolution, bottle vs source) is a faithful recreation. Grew actually exceeds brew on security (signing, sandboxing, manifests).

The main gaps are:

1. **Build flexibility** — the hardcoded `configure/make/make install` vs brew's arbitrary Ruby DSL is the biggest functional gap
2. **No build vs runtime dep distinction** — matters for complex dependency trees
3. **Ecosystem scale** — brew has thousands of formulas; grew is growing its core tap
