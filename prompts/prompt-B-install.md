# Prompt B — cmd/install: add --formula flag and auto-detect

## Goal

Update `cmd/install/install.go` to add a `--formula` flag and auto-detect whether each named package is a formula or cask when neither `--cask` nor `--formula` is given.

## Context

`pkg/context.(*Context).ResolveKind(name string, forceCask, forceFormula bool) (isCask bool, err error)` has been added to `pkg/context/context.go` (Prompt A). Its semantics:
- `forceFormula=true`: only formulas; error if not found
- `forceCask=true`: only casks; error if not found
- neither: try formula first; fall back to cask; error if neither exists

The current `cmd/install/install.go` dispatch logic is:
```go
if installCask {
    // validate cask-incompatible flags...
    // pre-validate all names with ctx.LoadCask(name)
    // install with installer.CaskInstall(ctx, name, ...)
    return nil
}
// formula path: depgraph.Resolver.Resolve(name) + installer.InstallFormula(f, ctx, opts)
```

The `RunInstall` function receives `args []string`, acquires a `*context.InstallContext` (which embeds `*Context`), and installs all packages in one call.

## Changes required

### 1. Add `installFormula` variable

In the `var` block at the top of the file, add:
```go
installFormula bool
```

### 2. Register the flag in `init()`

After the `--cask` flag registration line, add:
```go
Command.Flags().BoolVar(&installFormula, "formula", false, "Operate on a formula even if a cask with the same name exists.")
```

### 3. Add mutual exclusivity check in `RunInstall`

After the existing `if installOnlyDependencies && installIgnoreDeps` check, add:
```go
if installCask && installFormula {
    return fmt.Errorf("--cask and --formula are mutually exclusive")
}
```

### 4. Restructure the dispatch logic

Replace the existing `if installCask { ... } else { ... }` block with a two-phase approach:

**Phase 1 — Resolve each name to a kind (formula or cask):**
```go
type resolvedPkg struct {
    name   string
    isCask bool
}
resolved := make([]resolvedPkg, 0, len(remaining))
for _, name := range remaining {
    isCask, err := ctx.ResolveKind(name, installCask, installFormula)
    if err != nil {
        return err
    }
    resolved = append(resolved, resolvedPkg{name, isCask})
}
```

**Phase 2 — Validate flag compatibility and install:**

For each resolved package:
- If `isCask`: validate that formula-only flags are not set (`--build-from-source`, `--force-bottle`, `--only-dependencies`, `--ignore-dependencies`, `--skip-post-install`, `--require-sha`). Return an appropriate error if any are set.
- If `isCask`: call `installer.CaskInstall(ctx, name, installNoQuarantine, installForce, installSkipLink)`
- If `!isCask`: use the existing `depgraph.Resolver.Resolve(name)` + `installer.InstallFormula(f, ctx, opts)` flow

Preserve the existing behavior:
- Formula-only flags (`--build-from-source`, `--force-bottle`, etc.) still work exactly as before when operating on formulas
- `--dry-run` still applies to both formulas and casks as before
- Dependency resolution (topological sort) still applies to formulas
- The two-phase pattern (resolve all first, then install all) should be maintained: resolve + pre-validate in one loop, install in a second loop

### 5. Update usage strings

Update `Use:` to `"install [flags] <formula|cask>..."` and update the error message for missing args:
```go
return fmt.Errorf("usage: grew install [--cask|--formula] <name>...")
```

Update `Long:` to mention auto-detection.

## Important invariants to preserve

- `ctx` is `*context.InstallContext`. `ctx.ResolveKind` works because `InstallContext` embeds `*Context`.
- The `depgraph.Resolver` is constructed with `ctx.Loader` and `ctx.LoadFormula` — do not change this.
- All existing flag semantics are unchanged; only the dispatch path is updated.
- The cask path calls `installer.CaskInstall` which internally calls `ctx.LoadCask` — no pre-loading of the cask object is needed in the command layer.

## Verify

```bash
go test -tags devmode -race ./cmd/install/... ./pkg/...
```

No new test files are required for this prompt. The integration tests in Prompt D will cover the end-to-end behavior.
