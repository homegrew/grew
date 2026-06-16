---
name: add-doctor-check
description: Add a new diagnostic check to grew doctor. Use when the user wants to add a health check, diagnostic, or validation to the `grew doctor` command. Follows the registration-via-init() pattern so the core flow is never edited directly.
user-invocable: true
---

# /add-doctor-check — Add a new grew doctor check

Arguments: `$ARGUMENTS` (description of the check, e.g. "verify tap signatures are valid")

---

## What to do

You are using the `package-engineer` agent role. Follow grew's doctor check convention: register via `init()`, never edit the core check list directly.

### 1. Understand the check

If `$ARGUMENTS` is empty or vague, ask the user:
- What condition does this check verify?
- Is it macOS-specific or cross-platform?
- What is the remediation if the check fails?

### 2. Read existing checks for style

Read one existing check file:
```bash
ls pkg/doctor/
```
Pick a `doctor_darwin.go` (platform-specific) or a `*_check.go` file and read it. Match the struct shape and registration pattern exactly.

### 3. Determine the file

- **macOS-only check** (Seatbelt, quarantine, notarization, codesign, launchctl): create or append to `pkg/doctor/doctor_darwin.go`
- **Cross-platform check**: create a new file `pkg/doctor/<name>_check.go` with no build tag

### 4. Implement the check

A check must implement the `doctor.Check` interface (read the interface definition from `pkg/doctor/`).

Typical structure:
```go
type <Name>Check struct{}

func (c <Name>Check) Name() string        { return "<human-readable name>" }
func (c <Name>Check) Category() string    { return "<category>" }
func (c <Name>Check) Run(ctx *context.Context) doctor.Result {
    // ... check logic ...
    if <problem> {
        return doctor.Fail("<what's wrong>", "<how to fix it>")
    }
    return doctor.Pass()
}

func init() {
    doctor.Register(<Name>Check{})
}
```

Rules:
- Keep check logic self-contained — one check, one concern
- Accept `*context.Context`; use `ctx.Paths.*` for any path references
- `doctor.Fail` message: what is wrong. Remediation: what the user should do.
- Never call `os.Exit` or `log.Fatal` inside a check
- Checks must be idempotent and side-effect-free (read-only)

### 5. Build and verify

```bash
make dev 2>&1 | tail -5
./grew setup --unsafe 2>/dev/null; ./grew doctor 2>&1 | grep -A2 "<check name>"
```

The new check name must appear in `grew doctor` output.

### 6. Write a unit test

In `pkg/doctor/<name>_check_test.go`:
```go
func Test<Name>Check_Pass(t *testing.T) { ... }
func Test<Name>Check_Fail(t *testing.T) { ... }
```

Run it:
```bash
go test -tags devmode -race -run Test<Name> ./pkg/doctor/
```

### 7. Report back

Tell the user:
- File created/modified
- Check name as it appears in `grew doctor`
- How to reproduce the failure case for manual testing
