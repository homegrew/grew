---
name: security-audit
description: Run a security audit on the current branch's changes. Use when the user asks to audit, security-review, or check for vulnerabilities in pending changes. Spawns the security-auditor agent against the diff.
user-invocable: true
---

# /security-audit — Audit pending changes for security issues

Arguments: `$ARGUMENTS` (optional: a specific file or package path to focus on)

---

## What to do

You are acting as the `security-auditor` agent. Review the pending changes (or the specified path) for security issues specific to grew's threat model.

### 1. Get the diff

```bash
git diff main...HEAD --stat
git diff main...HEAD
```

If `$ARGUMENTS` names a file or package, also read it in full:
```bash
cat $ARGUMENTS
```

If there are no changes vs main, audit the working tree instead:
```bash
git diff HEAD
```

### 2. Identify the trust surface

For each changed file, classify it:
- **Network boundary**: `pkg/downloader`, `pkg/tap`, `pkg/osvdev`, any HTTP client usage
- **Filesystem boundary**: `pkg/safepath`, `pkg/fsutil`, `pkg/cellar`, `pkg/caskroom`, `pkg/linker`, `pkg/relocation`, archive extraction
- **Process boundary**: `pkg/sandbox`, `pkg/sudo`, any `exec.Command` usage
- **Cryptographic**: `pkg/signing`, `pkg/snapshot`, hash verification, key loading
- **CLI input**: `cmd/*/`, flag parsing, argument validation

### 3. Apply grew-specific checks

For each trust boundary touched, check:

**Downloads & verification**
- [ ] Both SHA256 and SHA512 are checked (not just one)
- [ ] Hashes verified *before* extraction, not after
- [ ] HTTPS enforced; HTTP URLs rejected at parse time
- [ ] `HOMEGREW_ALLOWED_HOSTS` allowlist respected

**Path safety**
- [ ] All paths through `pkg/safepath` before use
- [ ] Archive extraction rejects `../` and absolute paths (Zip Slip)
- [ ] Symlink creation validates target is within prefix

**External commands**
- [ ] `--` separator passed to `git`, `hdiutil`, `tar`, `launchctl`
- [ ] No `exec.Command("sh", "-c", <user-input>)`
- [ ] Arguments passed positionally, not via shell string interpolation

**Sandboxing**
- [ ] Build scripts and post-install hooks run under Seatbelt
- [ ] Sandbox profile does not allow writes to `$HOME`

**Privileges**
- [ ] `sudo` only for initial prefix setup
- [ ] `--unsafe` flag gated by devmode build constraint, not env var

**Self-update**
- [ ] Binary patches dual-hash verified before apply
- [ ] OSV check fails closed (error = abort)
- [ ] Atomic replacement: no partial binary left on failure

### 4. Check for OWASP-class issues

Even outside grew-specific checks, flag:
- Command injection via unsanitized user input in subprocess calls
- Path traversal in any file operation
- SSRF via user-controlled URLs
- Privilege escalation via file permission mistakes

### 5. Output findings

For each finding:

**Severity**: Critical / High / Medium / Low / Info
**File:line**: exact location
**What**: one sentence
**Why it matters**: concrete exploit or impact
**Fix**: specific code change

End with: `N critical, N high, N medium, N low findings.`

If no issues found, say so explicitly.
