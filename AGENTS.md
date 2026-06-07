# grew AI Agent Instructions (Updated)

This file serves as the definitive guide for AI-assisted coding agents working on the `grew` package manager. The goal is to ensure that any contribution adheres to the project's advanced security model, modular architecture, and development best practices.

## 🚀 What is grew?
`grew` is a Go-based, macOS-focused package manager designed for determinism, security, and simplicity. It aims to be a modern, hardened alternative to Homebrew. Key features include:
*   **Security:** Ed25519 bottle signing, dual-hash verification (SHA256/SHA512), sandboxed builds, and mandatory macOS Quarantine attribute application.
*   **Determinism:** Support for `grew lock` to pin exact versions and dependency trees.
*   **Safety:** Per-file SHA256 manifests (`.MANIFEST.json`) and installation receipts (`INSTALL_RECEIPT.json`) provide full provenance tracking.

## 🛠️ Recommended Developer Workflow (The Golden Path)
Always follow these steps when contributing or testing:
1.  **Build**: `make build` (For release builds).
2.  **Developer Mode Build**: `make dev` (Required for local development/testing of `--unsafe` features).
3.  **Testing**: Run comprehensive tests using the dedicated suites:
    *   Unit Tests: `make test-unit`
    *   Integration Tests: `make test-integration`
    *   Smoke Tests: `make test-smoke`
    *   End-to-End (E2E): `make test-e2e` (Use this for full lifecycle validation).
4.  **Code Quality**: Run `make fmt` and `make lint`.
5.  **Verification**: After making changes, run `grew doctor` to check structural integrity and security compliance.

## 📂 Repository Structure & Conventions
The codebase is highly modular, which must be respected:

*   **CLI Commands:** New subcommands must live in a dedicated package under `cmd/<name>/`. Each must export a `Command *cobra.Command` variable and include a `doc.go` file with a package-level description.
*   **Shared State:** All commands MUST use the centralized state management provided by `pkg/context`. Do not rely on global variables; utilize the `Context` object passed through the command execution chain.
*   **Core Logic Separation:** Complex, reusable logic (e.g., dependency resolution, path manipulation) must reside in dedicated packages within `pkg/` (e.g., `pkg/depgraph`, `pkg/linker`).

## 🛡️ Critical Architectural Concepts to Master
AI agents must understand these concepts as they define the boundaries of safe modification:

1.  **Context Management (`pkg/context`)**: This package is the single source of truth for system state (paths, loaders, etc.). All operations should read from or write through this context object. See `docs/tech.md` for details on its components.
2.  **Self-Update Mechanism**: The update process is complex and multi-layered. Agents must be aware that `grew` performs:
    *   Source-based updates via Git/Go build.
    *   Release-based updates using **Multi-Hop Binary Patching (`bspatch`)**.
    *   **Security Gates**: The update process includes mandatory checks for OSV.dev vulnerabilities and runs the new binary in a restricted sandbox *before* atomic replacement. (See `docs/tech.md` for full details).
3.  **Sandboxing & Security:** Never assume an operation is safe. All external commands must be hardened using `--` end-of-options separators to prevent shell injection. The system uses macOS Seatbelt and strict path validation (`pkg/safepath`) throughout the install lifecycle.

## 💡 Key Development Guidelines (The "How To")
*   **Principle of Least Privilege:** Only use `sudo` when absolutely necessary for initial prefix setup; all runtime operations should be rootless.
*   **Documentation Linkage:** When adding new features, document them in the relevant command's `doc.go`, but reference high-level concepts (like "Security Model" or "Context Usage") by linking to existing documentation files (`docs/tech.md`). **Do not duplicate large blocks of text.**
*   **Testing Strategy:** Always write tests for new logic, covering unit, integration, and E2E scenarios. Use `make check-all` as the final verification step.

## ⚠️ What to Avoid (Anti-Patterns)
*   Bypassing `pkg/context` or assuming global state availability.
*   Ignoring the security checks in the self-update path.
*   Hardcoding paths instead of using context-provided variables (`Context.Paths.Cellar`).
