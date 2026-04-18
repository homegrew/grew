# Architecture & Technical Details

This document explains some of the internal mechanics of `grew`, specifically focusing on bootstrapping, updates, and developer workflows.

## 1. How `grew` installs itself

Because `grew` manages dependencies and system environments, its initial installation needs to be deterministic and safe. The bootstrap process is handled by a standalone Go tool called `getgrew`.

**The Bootstrapping Flow:**

1. **Get the Installer:** The user downloads the bootstrapping tool via `go install github.com/homegrew/grew/tools/getgrew@latest`. This provides a lightweight executable `getgrew`.
2. **Fetch Release:** When the user runs `getgrew`, it communicates with the GitHub API to find the latest stable release of `grew`. It identifies the appropriate pre-compiled binary asset for the host's OS and Architecture (e.g., `grew_Darwin_arm64.tar.gz`).
3. **Verify Checksums:** `getgrew` fetches the `checksums.txt` file from the release, computes the SHA256 hash of the downloaded tarball, and strictly verifies it against the published hash.
4. **Extract & Stage:** The binary is extracted from the archive directly into memory (with path traversal/Zip Slip protections enforced) and placed alongside the `getgrew` executable (or in the current working directory).
5. **System Setup:** The user then runs the newly downloaded binary using `./grew setup` (usually via `sudo`). The `setup` command initializes the system prefix (`/opt/homegrew` on Apple Silicon, `/usr/local/homegrew` on Intel/Linux).
6. **Clone & Build:** By default, `setup` attempts to `git clone` the `grew` repository into `<prefix>/Grew` and build the final executable from source using `go build`. This ensures the installed binary perfectly matches the local repository. If `git` or `go` are not available, it safely falls back to copying the downloaded executable directly into `<prefix>/bin/grew`.
7. **Permissions:** After creating the prefix and moving the binary, `setup` transfers ownership of the directory structure to the user who invoked `sudo` (via the `SUDO_USER` environment variable). All subsequent `grew` commands run without root privileges.

## 2. How the update process works

`grew`'s self-update mechanism (`grew selfupdate`) is designed to atomically replace the running binary without leaving the package manager in a broken state.

The update strategy diverges based on how `grew` was initially set up:

**Source-Based Updates (Primary Strategy)**
If a valid git repository exists at `<prefix>/Grew` (created during `setup`), the update proceeds via source compilation:
1. `grew` invokes `git fetch` to retrieve the latest changes from the origin repository.
2. If `HOMEGREW_TAP_VERIFY` is enabled, it verifies the cryptographic signatures of the remote commits.
3. It performs a fast-forward merge via `git reset --hard origin/main`.
4. It executes `go build -o <prefix>/bin/grew .` inside the repository.
5. The executable is atomically replaced.

**Release-Based Updates (Fallback Strategy)**
If the source repository does not exist, `grew` falls back to fetching a pre-compiled binary:
1. `grew` hits the `https://api.github.com/repos/homegrew/grew/releases` endpoint to identify the latest stable tag.
2. It downloads the platform-specific release asset and its corresponding `checksums.txt`.
3. It validates the SHA256 checksum of the downloaded archive.
4. It extracts the new executable into a temporary file in the prefix.
5. `grew` uses `os.Rename` to atomically overwrite its currently running executable (`<prefix>/bin/grew`) with the new version.

## 3. Developer Mode (`devmode`) Explained

Typically, `grew` requires root privileges (`sudo grew setup`) during initial setup to create system-level prefix directories (like `/opt/homegrew`), establishing a strict isolation boundary between the package manager and the user's `$HOME` directory.

However, requiring `sudo` is a major friction point for local development, testing, and continuous integration workflows. To solve this, `grew` includes a **Developer Mode**.

**What is `devmode`?**
Devmode is a combination of a compile-time build tag and a runtime CLI flag that enables user-local, rootless installations.

**How it works:**
1. **Compile-time Gate:** You must compile the binary with the `devmode` build tag: `go build -tags devmode`. In the codebase, this tag triggers the inclusion of `internal/runtime/devmode_on.go`, which sets the constant `runtime.DevMode = true`. (Release builds use `devmode_off.go` where `runtime.DevMode = false`).
2. **Runtime Gate:** You must pass the `--unsafe` flag to the setup command: `./grew setup --unsafe`.
3. **Evaluation:** When `grew` initializes, `runtime.devModeActive()` checks that *both* conditions are met (`DevMode && Unsafe`).

If devmode is active, `grew` bypasses the standard `sudo` requirements and instead sets the prefix to a hidden directory in the user's home folder: `~/.homegrew`. This allows developers to test the full lifecycle of the package manager—including sandboxed extraction, cellar linking, and dependency resolution—without ever escalating privileges or modifying system directories.

*Note: Release builds ignore the `--unsafe` flag entirely. If a user attempts to run `grew setup --unsafe` on a production binary, it will fail and demand `sudo`.*
