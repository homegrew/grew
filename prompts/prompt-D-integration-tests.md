# Prompt D — Integration tests for auto-detect and --formula flag

## Goal

Add integration tests covering the new auto-detection and `--formula` flag behavior for `grew install`, `grew uninstall`, and `grew info`.

## Context

Prompts A–C have:
- Added `pkg/context.(*Context).ResolveKind`
- Added `--formula` flag to `install`, `uninstall`, `info`
- Made each command auto-detect formula vs cask when neither flag is given

Existing test infrastructure:
- `tests/integration/` — Go integration tests, each builds a `testbin` binary and exercises it via `exec.Command`
- `tests/testhelper/` — helpers: `SetupPrefix`, `BuildTestBinary`, `CreateFormula`, `ComputeSHA256`
- `tests/integration/install_test.go` — formula install; use as reference for mock HTTP server setup
- `tests/integration/cask_test.go` — cask install (`TestCaskInstallIntegration`); use as reference for mock cask server and fixture ZIP

The `tests/testbin/main.go` binary delegates all commands to the real CLI router. It does NOT need changes for this feature.

Existing fixtures:
- `dummy` formula — referenced in install_test.go; a minimal formula with a mock tar.gz bottle
- `dummycask` cask — referenced in cask_test.go; a minimal cask with a mock ZIP app

## Task

### Create `tests/integration/autodetect_test.go`

This file must not import any packages outside the Go standard library and the grew module. Follow the patterns in `install_test.go` and `cask_test.go` exactly.

### Required test cases

#### 1. `TestAutoDetect_FormulaNoAmbiguity`

Setup: a formula named `auto-formula` exists; no cask with that name.

Action: `grew install auto-formula` (no flags).

Expected: install succeeds; the formula is installed into the cellar.

#### 2. `TestAutoDetect_CaskFallback`

Setup: a cask named `auto-cask` exists; no formula with that name.

Action: `grew install auto-cask` (no flags).

Expected: install succeeds; the cask is installed (Dummy.app present in Applications dir).

#### 3. `TestAutoDetect_FormulaWinsWhenBoth`

Setup: both a formula and a cask exist with the same name `both-pkg`.

Action: `grew install both-pkg` (no flags).

Expected: install succeeds and installs the **formula** (not the cask).

#### 4. `TestAutoDetect_FormulaFlag_Success`

Setup: a formula named `formula-only` exists.

Action: `grew install --formula formula-only`.

Expected: succeeds and installs the formula.

#### 5. `TestAutoDetect_FormulaFlag_NoMatch`

Setup: a cask named `cask-only` exists; no formula with that name.

Action: `grew install --formula cask-only`.

Expected: exits non-zero; stderr contains "not found" or "formula".

#### 6. `TestAutoDetect_CaskFlag_NoMatch`

Setup: a formula named `formula-only` exists; no cask with that name.

Action: `grew install --cask formula-only`.

Expected: exits non-zero; stderr contains "not found" or "cask".

#### 7. `TestAutoDetect_MutuallyExclusive`

Setup: any valid package name (or none).

Action: `grew install --cask --formula somepkg`.

Expected: exits non-zero; stderr contains "mutually exclusive".

#### 8. `TestAutoDetect_UninstallCaskFallback`

Setup: install a cask named `auto-cask` (without `--cask` flag), then uninstall it (without `--cask` flag).

Expected: uninstall succeeds; the cask is removed.

#### 9. `TestAutoDetect_InfoCaskFallback`

Setup: install a cask named `auto-cask`, then run `grew info auto-cask` (without `--cask` flag).

Expected: exits zero; stdout contains the cask name.

### Implementation notes

- Reuse the mock HTTP server pattern from `install_test.go` for serving formula bottles.
- Reuse the mock cask server pattern from `cask_test.go` for serving cask ZIPs.
- Use `testhelper.SetupPrefix`, `testhelper.CreateFormula`, `testhelper.BuildTestBinary`.
- For cask fixtures, follow how `TestCaskInstallIntegration` creates and serves the ZIP.
- The `HOMEGREW_NO_INIT_TAP=1` env var suppresses core tap cloning — set it on all test commands.
- Prefix must be isolated per test using `t.TempDir()`.

### Verify

```bash
go test -tags devmode ./tests/integration/... -run TestAutoDetect -v
```

All 9 tests must pass. Do not modify any existing test files.
