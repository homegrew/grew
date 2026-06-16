---
name: test
description: Run grew's test suite at the appropriate level. Use when the user asks to run tests, check if tests pass, or verify a change didn't break anything. Accepts an optional level argument (unit, integration, smoke, e2e, all) and an optional package path.
user-invocable: true
---

# /test — Run grew tests

Arguments: `$ARGUMENTS` (optional: `unit`, `integration`, `smoke`, `e2e`, `all`, or a Go package path like `./pkg/safepath`)

---

## What to do

### 1. Parse arguments

- No args → run `unit` (fast, safe default)
- `unit` → `make test-unit`
- `integration` → `make test-integration`
- `smoke` → `make test-smoke`
- `e2e` → warn the user this installs real formulas and takes several minutes, then confirm before running `make test-e2e`
- `all` → `make check-all`
- A Go import path (starts with `./` or contains `/`) → run that single package

### 2. Single-package run

If a package path was given (e.g. `./pkg/safepath` or `safepath`):
```bash
go test -tags devmode -race -run . ./pkg/<package>/
```
If the user also named a test function (e.g. `safepath TestJoin`), pass it via `-run`:
```bash
go test -tags devmode -race -run TestJoin ./pkg/safepath/
```

### 3. Run and report

Run the chosen make target or go test command. Stream output to the user.

After completion:

**If all pass:**
Tell the user which suite passed and how many tests ran (parse `ok` lines from output).

**If any fail:**
- Show the failing test names and the failure message verbatim
- Identify which file:line the failure is at
- If the failure looks like a compilation error, run `make build` to get a cleaner error message
- Suggest a fix if the cause is obvious

### 4. Notes to share

- Unit tests require the `devmode` build tag — they will silently skip or fail to compile without it. `make test-unit` handles this automatically.
- Integration and smoke tests compile a proxy binary from `tests/testbin/` and exec it — they don't use the Go test runner as the binary under test. See `tests/README.md` for details.
- `make test-e2e` installs real formulas from GitHub and takes several minutes. Only suggest it for pre-release verification.
