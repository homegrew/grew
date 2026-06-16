---
name: add-command
description: Scaffold a new grew subcommand end-to-end. Use when the user asks to add, create, or scaffold a new CLI command. Reads existing commands for style reference, creates cmd/<name>/ with Command + doc.go, registers in root.go, and updates cmd/README.md.
user-invocable: true
---

# /add-command — Scaffold a new grew subcommand

Arguments: `$ARGUMENTS` (the command name, e.g. `pin` or `audit`)

---

## What to do

You are using the `cli-developer` agent role. Follow grew's command convention exactly.

### 1. Validate the name

If `$ARGUMENTS` is empty, ask the user for the command name before proceeding.

The name must be lowercase, no spaces. Check that `cmd/<name>/` does not already exist:
```bash
ls cmd/<name>/ 2>/dev/null && echo EXISTS || echo OK
```

### 2. Read reference commands for style

Read two existing small commands to match style:
- `cmd/leaves/` — simple read-only command
- `cmd/desc/` — command that accepts an argument

Read both the main `.go` file and `doc.go` in each.

### 3. Create cmd/<name>/doc.go

```go
// Package <name> implements the grew <name> command.
package <name>
```

### 4. Create cmd/<name>/<name>.go

Follow this structure exactly:
- Package name matches directory name
- Export a single `Command` variable of type `*cobra.Command`
- Use `pkg/context` to get paths/loaders — never hardcode paths
- Read-only commands: `context.Load(cmd)` — destructive: `context.LoadInstall(cmd)`
- Output via `pkg/logger` or `pkg/ui`, not `fmt.Println`
- Keep the file under ~80 lines; extract any logic to `pkg/`

Minimum template:
```go
package <name>

import (
    "github.com/spf13/cobra"
    "grew/pkg/context"
    "grew/pkg/logger"
)

var Command = &cobra.Command{
    Use:   "<name> [args]",
    Short: "<one-line description>",
    Long:  `<longer description matching doc.go>`,
    RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
    ctx, err := context.Load(cmd)
    if err != nil {
        return err
    }
    _ = ctx
    logger.Info("<name> not yet implemented")
    return nil
}
```

### 5. Register in root.go

Find the `AddCommands` call or the command list in `root.go` / `pkg/cli/`. Add the import and register `<name>.Command` in alphabetical order among existing commands.

### 6. Update cmd/README.md

Add a row for the new command in the table. Match the existing format exactly.

### 7. Build and verify

```bash
make dev 2>&1 | tail -5
./grew <name> --help
```

The `--help` output must show the correct usage and description.

### 8. Report back

Tell the user:
- Files created (with paths)
- Where it was registered
- What the user should implement next in `RunE`
