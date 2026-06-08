# Prompt C — cmd/uninstall + cmd/info: add --formula flag and auto-detect

## Goal

Update `cmd/uninstall/uninstall.go` and `cmd/info/info.go` to add a `--formula` flag and auto-detect whether each named package is a formula or cask when neither flag is given.

## Context

`pkg/context.(*Context).ResolveKind(name string, forceCask, forceFormula bool) (isCask bool, err error)` has been added to `pkg/context/context.go` (Prompt A). Its semantics:
- `forceFormula=true`: only formulas; error if not found
- `forceCask=true`: only casks; error if not found
- neither: try formula first; fall back to cask; error if neither exists

---

## Changes to `cmd/uninstall/uninstall.go`

### Current dispatch logic

```go
var uninstallCask bool

func init() {
    Command.Flags().BoolVar(&uninstallCask, "cask", false, "Uninstall a cask instead of a formula.")
    // ...
}

func runUninstall(args []string) error {
    // ...
    if uninstallCask {
        for _, name := range args {
            if err := installer.CaskUninstall(ctx, name, uninstallForce); err != nil {
                return err
            }
        }
        return nil
    }
    // formula path: installCtx.UninstallFormula(name, uninstallForce)
}
```

### Required changes

1. Add `var uninstallFormula bool`

2. In `init()`, add after the `--cask` flag:
   ```go
   Command.Flags().BoolVar(&uninstallFormula, "formula", false, "Uninstall a formula even if a cask with the same name exists.")
   ```

3. In `runUninstall`, add after args validation:
   ```go
   if uninstallCask && uninstallFormula {
       return fmt.Errorf("--cask and --formula are mutually exclusive")
   }
   ```

4. Replace `if uninstallCask { ... } else { ... }` with per-name resolution.

   `runUninstall` acquires a `*context.InstallContext` (assigned to `ctx`). `InstallContext` embeds `*Context`, so `ctx.ResolveKind(...)` is available directly.

   New loop:
   ```go
   for _, name := range args {
       isCask, err := ctx.ResolveKind(name, uninstallCask, uninstallFormula)
       if err != nil {
           return err
       }
       if isCask {
           if err := installer.CaskUninstall(ctx, name, uninstallForce); err != nil {
               return err
           }
       } else {
           if err := installCtx.UninstallFormula(name, uninstallForce); err != nil {
               return err
           }
       }
   }
   ```

   Check whether the existing code uses `ctx` or `installCtx` as the variable name for `*context.InstallContext` and follow the existing convention.

5. Update `Use:` to `"uninstall [flags] <formula|cask>..."`.

---

## Changes to `cmd/info/info.go`

### Current dispatch logic

```go
var infoCask bool

func init() {
    Command.Flags().BoolVar(&infoCask, "cask", false, "Show cask info")
    // ...
}

func runInfo(args []string) error {
    // ...
    if infoCask {
        for i, name := range args {
            c, err := ctx.LoadCask(name)
            // ...
            cask.PrintInfoWithData(c, ver)
        }
        return nil
    }
    // formula path
    for i, name := range args {
        f, err := ctx.LoadFormula(name)
        // ...
    }
}
```

There is also a JSON path (guarded by `--json` flag) inside `runInfo` that branches on `infoCask`. Apply the same changes there.

### Required changes

1. Add `var infoFormula bool`

2. In `init()`, add after the `--cask` flag:
   ```go
   Command.Flags().BoolVar(&infoFormula, "formula", false, "Show formula info even if a cask with the same name exists.")
   ```

3. In `runInfo`, add early:
   ```go
   if infoCask && infoFormula {
       return fmt.Errorf("--cask and --formula are mutually exclusive")
   }
   ```

4. For each package name, replace the `if infoCask` dispatch with:
   ```go
   isCask, err := ctx.ResolveKind(name, infoCask, infoFormula)
   if err != nil {
       return err
   }
   if isCask {
       c, err := ctx.LoadCask(name)
       if err != nil {
           return err
       }
       cask.PrintInfoWithData(c, ver)
   } else {
       f, err := ctx.LoadFormula(name)
       if err != nil {
           return err
       }
       // existing formula print logic
   }
   ```

   Note: `ctx` in `runInfo` is a `*context.Context` (read-only, not InstallContext). `ResolveKind` is on `*Context` so it is directly available.

5. Apply the same `isCask` dispatch to the JSON output path.

6. Update `Use:` to `"info [flags] <formula|cask>..."`.

## Important invariants to preserve

- `infoCask` and `infoFormula` are mutually exclusive but not required; the auto-detect path handles neither.
- `ctx.LoadCask` and `ctx.LoadFormula` are called a second time after `ResolveKind` (which already called them internally). This is acceptable — loading is a cheap file read.
- Existing behavior for `--json` output must be preserved.
- No changes to output format.

## Verify

```bash
go test -tags devmode -race ./cmd/uninstall/... ./cmd/info/... ./pkg/...
```
