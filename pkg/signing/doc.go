// Package signing implements Ed25519 bottle signing and verification for grew.
//
// # What is signed
//
// Bottle integrity is anchored to the SHA256 hex digest of the downloaded
// archive. [Sign] produces a detached Ed25519 signature over the raw bytes of
// that hex string; [Verify] / [VerifyAny] check it. Signing the hex string
// rather than the raw bytes keeps the signed payload a fixed 64-character
// ASCII value and matches the representation already present in formula YAML.
//
// # Trust store
//
// Trusted public keys live in <prefix>/etc/trusted-keys, one key per line.
// Each line is either a hex-encoded raw 32-byte key or an SSH public key in
// the "ssh-ed25519 AAAA... comment" format produced by ssh-keygen(1).
// [LoadTrustedKeys] parses the file; [VerifyAny] accepts a signature if any
// loaded key verifies it.
//
// # Key encoding
//
// Private keys can be supplied as a 64-character hex seed or as the path to
// an unencrypted OpenSSH private key file (ssh-keygen -t ed25519).
// [DecodePrivateKey] auto-detects the format. Public keys can be serialised
// with [EncodePublicKey] (hex) or read back with [DecodePublicKey].
//
// # Typical operator workflow
//
//  1. Generate a key pair: [GenerateKey]
//  2. Add the hex-encoded public key to <prefix>/etc/trusted-keys
//  3. Sign a formula's SHA256 with `grew sign` (which calls [Sign])
//  4. At install time, [VerifyAny] checks the signature against all trusted keys
package signing
