# Plan: Universal Binary Migration

## Goal

Replace the two per-architecture release artifacts (`grew_Darwin_x86_64.tar.gz` and
`grew_Darwin_arm64.tar.gz`) with a single macOS universal (fat) binary
(`grew_Darwin_all.tar.gz`). Binary delta patches and checksum files follow the same
naming change. The self-update path in the running binary is updated to look for the
new names.

---

## Files in scope

| File | Change |
|------|--------|
| `.goreleaser.yaml` | Add `universal_binaries`, update `archives` template |
| `.github/workflows/goreleaser.yml` | No change needed (patcher called generically) |
| `pkg/release/release.go` | `normalizePlatform` returns `"all"` for arch; cascades to all naming helpers |
| `tools/patcher/main.go` | Remove per-platform loop; generate one patch for the universal binary |
| `pkg/cmd/selfupdate.go` | Likely no changes; verify nothing hardcodes arch |

---

## Transition note (important)

Users running an **old arch-specific binary** (e.g. `grew_Darwin_x86_64` or
`grew_Darwin_arm64`) upgrading to a **new universal binary** release will hit a silent
break:

1. Patch update fails — no `grew_Darwin_x86_64_*_to_*.patch` exists in the new release.
2. Full archive download also fails — `grew_Darwin_x86_64.tar.gz` no longer exists.

**Mitigation accepted for this plan:** the self-update fallback (`selfUpdateFromGit`)
already covers source installs, and `grew setup` reinstalls from the latest release.
We will add a clear error message when the archive name is not found so users know to
run `grew setup` once. No backward-compat shim; the old naming is dead code.

---

## Agent team

| Agent | Role (existing) | Work |
|-------|-----------------|------|
| A | `package-engineer` | `pkg/release/release.go` — rename helpers |
| B | `package-engineer` | `tools/patcher/main.go` — single-platform logic |
| C | `cli-developer` | `.goreleaser.yaml` — universal_binaries block |
| D | `cli-developer` | `pkg/cmd/selfupdate.go` — verify + improve error message |
| E | `security-auditor` | Review all diffs before merge |

No new roles are required; all four implementation agents exist in the registry.

---

## Phases

### Phase 1 — Parallel: foundation + config (A and C run simultaneously)

**Agent A — `pkg/release/release.go`**

`normalizePlatform()` currently returns `(runtime.GOOS, runtime.GOARCH)` after
normalisation. Because grew is macOS-only and we are switching to universal binaries,
change the returned arch unconditionally to `"all"`:

```go
func normalizePlatform() (osName, archName string) {
    return "Darwin", "all"
}
```

This single change fixes all downstream helpers without touching them:

- `AssetName()` → `grew_Darwin_all.tar.gz`
- `RawBinaryName()` → `grew_Darwin_all`
- `PatchName(old, new)` → `grew_Darwin_all_v0.X.0_to_v0.Y.0.patch`
- `ParsePatchVersion(name)` — prefix is now `grew_Darwin_all_`, same logic

Run `go test ./pkg/release/...` after the change.

---

**Agent C — `.goreleaser.yaml`**

Replace the existing `builds` + `archives` block with one that produces a single
universal binary archive.

```yaml
builds:
  - id: darwin-amd64
    env: [CGO_ENABLED=0]
    goos: [darwin]
    goarch: [amd64]
    flags: [-trimpath]
    ldflags: [-s -w -X main.Version={{ .Version }}]
    mod_timestamp: '{{ .CommitTimestamp }}'
  - id: darwin-arm64
    env: [CGO_ENABLED=0]
    goos: [darwin]
    goarch: [arm64]
    flags: [-trimpath]
    ldflags: [-s -w -X main.Version={{ .Version }}]
    mod_timestamp: '{{ .CommitTimestamp }}'

universal_binaries:
  - ids: [darwin-amd64, darwin-arm64]
    replace: true          # removes the two per-arch binaries from the archives

archives:
  - format: tar.gz
    name_template: >-
      {{ .ProjectName }}_
      {{- title .Os }}_
      {{- if eq .Arch "all" }}all{{ else }}{{ .Arch }}{{ end }}
    builds_info:
      group: root
      owner: root

checksum:
  name_template: 'checksums.txt'
```

Verify with `goreleaser check` locally (or via CI).

---

### Phase 2 — Parallel: patcher + selfupdate (B and D run simultaneously, after Phase 1)

Phase 2 can start as soon as Phase 1 commits land, but the agents can draft their
changes immediately with the known new naming.

**Agent B — `tools/patcher/main.go`**

Remove the `platforms` slice and the loop. The tool now handles exactly one "platform":
the universal Darwin binary.

Key changes:
- Delete `type platform struct` and `var platforms`.
- Replace the loop with a single block using:
  - `archiveName = "grew_Darwin_all.tar.gz"`
  - `rawBinName  = "grew_Darwin_all"`
  - `patchFileName = fmt.Sprintf("grew_Darwin_all_%s_to_%s.patch", prevRelease, newRelease)`
- `binaryChecksums.txt` still written once (now for one binary instead of two).
- Checksums per-patch (`.sha256` / `.sha512`) are unchanged in structure.

Run `go build ./tools/patcher/` to verify compilation.

---

**Agent D — `pkg/cmd/selfupdate.go`**

Audit the file for any hardcoded arch strings or platform-specific logic. Currently
the file delegates naming entirely to `pkg/release`, so the main task is:

1. Confirm no hardcoded `x86_64` / `arm64` strings.
2. Improve the error message when the archive is not found (transition case):

```go
// In the fallback path, if installer.InstallLatestRelease returns an error
// that contains "not found", add a hint:
if err != nil && strings.Contains(err.Error(), "not found") {
    fmt.Fprintln(os.Stderr,
        "==> Hint: the release format changed. Run `grew setup` to reinstall.")
}
```

(Exact placement: after line 82 in the current file.)

---

### Phase 3 — Security audit (Agent E, after Phase 1 and 2)

Agent E (`security-auditor`) reviews the combined diff for:

- Any new path or archive name string that could be user-controlled or spoofed.
- Whether the `normalizePlatform` hard-coding of `"all"` removes any meaningful
  runtime safety check (it does not — arch was never a security boundary here).
- Whether the removed platform loop in the patcher introduces any resource or
  injection risk.
- Checksum verification coverage is unchanged; confirm no regressions.

---

### Phase 4 — Test run

After all code changes are merged:

```bash
make test-unit           # pkg/release and pkg/bpatch unit tests
goreleaser check         # validate .goreleaser.yaml
go build ./tools/patcher/
```

For a full end-to-end smoke test the CI release pipeline must be triggered with a
test tag (e.g. `v0.0.0-test`); that is out of scope for this plan but should be done
before merging to main.

---

## Dependency graph

```
Phase 1A (release.go)  ─┬─► Phase 2B (patcher)
                         └─► Phase 2D (selfupdate)
Phase 1C (goreleaser)       (independent)

Phase 2B + 2D ──────────────► Phase 3 (security audit)
Phase 3 ────────────────────► Phase 4 (tests)
```

Phases 1A and 1C run in parallel. Phases 2B and 2D run in parallel (and can draft
against the known naming even before Phase 1 merges).
