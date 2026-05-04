# grew tests

This directory contains integration, smoke, and end-to-end (E2E) tests for the `grew` package manager.

## Directory Structure

- **`integration/`**: Comprehensive tests for individual commands and their interactions. These tests use a proxy binary to simulate real execution environments.
- **`smoke/`**: Quick health checks to verify that basic functionality (help, version, config) is working as expected.
- **`e2e/`**: Long-running tests that exercise the full lifecycle of the application, including installation of real formulas from GitHub.
- **`testbin/`**: The source code for the proxy binary used by integration and smoke tests.
- **`testhelper/`**: Shared utility functions and setup logic used across all test suites.

## Running Tests

Tests can be run using the `Makefile`:

```bash
make test-integration    # Run integration tests
make test-smoke          # Run smoke tests
make test-e2e            # Run end-to-end tests
make check-all           # Run all of the above + unit tests
```

## Testing Architecture

Many of `grew`'s core features—such as sandboxed archive extraction and atomic self-updates—rely on executing the "current running binary" (found via `os.Executable`). Inside a standard Go test environment, the "current binary" is a temporary test runner that lacks the embedded functionality of the actual `grew` CLI.

To circumvent this limitation, the tests use a specialized strategy:

1. They dynamically compile a small proxy binary (located in `tests/testbin`) into a temporary mock prefix.
2. This proxy binary imports and exposes the full internal routing logic of `grew`.
3. Tests execute commands using this proxy binary as a standalone process.

This approach ensures that operations like "re-execing for sandboxed extraction" or "replacing the binary on disk" accurately replicate real-world execution environments without corrupting the host system.
