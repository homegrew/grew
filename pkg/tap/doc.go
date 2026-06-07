// Package tap manages formula and cask repositories ("taps") for grew.
//
// A tap is a shallow git clone of a repository that contains YAML formula or
// cask definitions. Taps are stored under <prefix>/Taps/<user>/<repo>/ and
// identified by the "user/repo" shorthand (e.g. "homegrew/homegrew-taps").
//
// The core tap (homegrew/homegrew-taps) is cloned automatically on first use
// via Manager.EnsureCloned. Additional taps can be added with Manager.Add and
// removed with Manager.Remove. Manager.Update fetches and fast-forwards every
// installed tap.
//
// # Commit-signature verification
//
// The HOMEGREW_TAP_VERIFY environment variable controls how strictly tap
// updates are checked:
//
//   - "off"    — no verification (default)
//   - "warn"   — log a warning for unsigned HEAD commits, but continue
//   - "strict" — refuse to accept tap updates whose HEAD commit is unsigned
//
// Verification is performed by CheckAfterUpdate after every clone or fetch,
// using the host's GPG/SSH configuration. Operators who want to mandate signed
// taps should add trusted public keys and set HOMEGREW_TAP_VERIFY=strict.
//
// All git invocations use the "--" end-of-options separator and validate
// path components before constructing arguments to prevent injection via
// attacker-controlled tap names or repository paths.
package tap
