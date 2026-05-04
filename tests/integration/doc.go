//go:build integration

// Package tests contains integration and end-to-end (E2E) tests for the grew binary.
//
// These tests validate the correct interaction between various internal
// packages (e.g., config, cellar, sandbox, release) by exercising full
// CLI commands like "install" and "selfupdate".
//
// Testing Architecture:
//
// Many of grew's core features—such as sandboxed archive extraction and
// atomic self-updates—rely on executing the "current running binary"
// (found via os.Executable). Inside a standard Go test environment, the
// "current binary" is a temporary test runner that lacks the embedded
// functionality of the actual grew CLI.
//
// To circumvent this limitation, the integration tests use a specialized
// strategy:
//
//  1. They dynamically compile a small proxy binary (located in tests/testbin)
//     into a temporary mock prefix (e.g., /tmp/prefix/bin/grew).
//  2. This proxy binary imports and exposes the full internal routing logic
//     of grew (e.g., cmd.Run, cmd.RunSelfUpdate).
//  3. Tests execute commands using this proxy binary as a standalone process.
//
// This approach ensures that operations like "re-execing for sandboxed
// extraction" or "replacing the binary on disk" accurately replicate
// real-world execution environments without corrupting the host system.
package integration
