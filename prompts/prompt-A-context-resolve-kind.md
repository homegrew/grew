# Prompt A — pkg/context: add ResolveKind

## Goal

Add a `ResolveKind` method to `*Context` in `pkg/context/context.go`, plus unit tests.

## Context

`grew` is a macOS package manager. A "formula" is a CLI tool; a "cask" is a GUI app. Currently commands like `install` and `uninstall` require the user to pass `--cask` to operate on casks. The goal is to support auto-detection: try formula first, fall back to cask when no formula found.

`pkg/context/context.go` already has:
- `func (ctx *Context) LoadFormula(name string) (*formula.Formula, error)` — loads from local tap or Homebrew API
- `func (ctx *Context) LoadCask(name string) (*cask.Cask, error)` — same for casks

Both return `non-nil error` when the package is not found.

## Task

### 1. Add the method to `pkg/context/context.go`

Insert after the `LoadCask` method:

```go
// ResolveKind determines whether name refers to a formula or a cask.
// forceCask and forceFormula must not both be true (callers enforce this).
//   - forceFormula=true: only search formulas; error if not found.
//   - forceCask=true:    only search casks; error if not found.
//   - neither:           formula wins; cask is the fallback; error if neither.
func (ctx *Context) ResolveKind(name string, forceCask, forceFormula bool) (isCask bool, err error) {
	if forceFormula {
		_, err = ctx.LoadFormula(name)
		return false, err
	}
	if forceCask {
		_, err = ctx.LoadCask(name)
		return true, err
	}
	if _, err = ctx.LoadFormula(name); err == nil {
		return false, nil
	}
	if _, err = ctx.LoadCask(name); err == nil {
		return true, nil
	}
	return false, fmt.Errorf("%s: no formula or cask found", name)
}
```

No new imports needed — `fmt` is already imported.

### 2. Add unit tests

Create or extend `pkg/context/context_test.go`.

Use table-driven subtests. To avoid depending on real taps or the Homebrew API, set up minimal fixture YAML files in a temp `TapDir` using `t.TempDir()` and `os.MkdirAll` / `os.WriteFile`. Wire them up via `formula.Loader{TapDir: ...}` and `cask.Loader{TapDir: ...}` and construct a `Context` directly:

```go
ctx := &Context{
    Loader:     &formula.Loader{TapDir: tapDir},
    CaskLoader: &cask.Loader{TapDir: tapDir},
    // other fields can be zero
}
```

A minimal formula YAML to place at `<tapDir>/homegrew/homegrew-taps/core/myformula.yaml`:
```yaml
name: myformula
version: "1.0"
homepage: https://example.com
description: test
url: https://example.com/myformula-1.0.tar.gz
sha256: "aabbcc"
```

A minimal cask YAML to place at `<tapDir>/homegrew/homegrew-taps/cask/mycask.yaml`:
```yaml
token: mycask
version: "1.0"
url: https://example.com/mycask-1.0.dmg
sha256: "aabbcc"
name:
  - My Cask
homepage: https://example.com
artifacts:
  - app: MyCask.app
```

Cover these cases:

| Test name | forceCask | forceFormula | fixture | expected isCask | expected err |
|---|---|---|---|---|---|
| ForceFormula_Found | false | true | formula exists | false | nil |
| ForceFormula_NotFound | false | true | no formula | false | non-nil |
| ForceCask_Found | true | false | cask exists | true | nil |
| ForceCask_NotFound | true | false | no cask | true | non-nil |
| Auto_FormulaWins | false | false | both exist | false | nil |
| Auto_CaskFallback | false | false | only cask | true | nil |
| Auto_NeitherFound | false | false | neither | false | non-nil |

### 3. Verify

```bash
go test -tags devmode -race ./pkg/context/...
```

All 7 subtests must pass. Do not modify any other files.
