package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildConfig describes the paths the build is allowed to write to.
type BuildConfig struct {
	BuildDir string   // source tree (read-write)
	KegDir   string   // install prefix (read-write)
	DepPaths []string // dependency cellar/opt dirs (read-only; informational on macOS)
}

// Command wraps a build step in platform-specific sandboxing.
//
// Security model:
//   - Network access is denied (source is already downloaded & verified).
//   - File writes are restricted to the build dir, keg dir, and system
//     temp directories needed by compilers.
//   - File reads are unrestricted (builds need system headers, toolchains,
//     dyld shared cache, etc.).
//   - Environment is scrubbed to essential build variables only.
//
// On macOS it uses sandbox-exec (Seatbelt). On Linux it uses a tiered
// approach: bubblewrap (bwrap) for full namespace isolation, falling back
// to unshare(1) for network+mount namespace isolation, and finally a
// clean environment as a last resort.
func Command(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	return platformCommand(cfg, name, args...)
}

// PostInstallConfig describes the restricted sandbox for post-install scripts.
// Unlike BuildConfig, post-install scripts get:
//   - Network access denied
//   - Write access ONLY to a temporary directory (not the keg itself)
//   - Read access to the keg and system paths
//   - Minimal environment (no compiler vars)
type PostInstallConfig struct {
	KegDir string // keg path (read-only)
	TmpDir string // writable scratch space for the script
}

// PostInstallCommand wraps a post-install step in platform-specific sandboxing.
// This is stricter than Command: the keg is read-only and only a temp dir
// is writable. Network access is denied.
func PostInstallCommand(cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	return platformPostInstallCommand(cfg, name, args...)
}

// postInstallEnv returns a minimal environment for post-install scripts.
// Only PATH, HOME, LANG, and TMPDIR are passed through. No compiler
// variables, no secrets.
func postInstallEnv(cfg PostInstallConfig) []string {
	allow := map[string]bool{
		"PATH": true, "HOME": true,
		"LANG": true, "LC_ALL": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if allow[key] {
			env = append(env, kv)
		}
	}
	env = append(env, "TMPDIR="+cfg.TmpDir)
	return env
}

// cleanEnv returns a minimal environment suitable for building.
// It strips all variables except essential build/compiler ones,
// preventing accidental leakage of secrets or tokens.
func cleanEnv(cfg BuildConfig) []string {
	allow := map[string]bool{
		"PATH": true, "HOME": true,
		"CC": true, "CXX": true, "CPP": true,
		"CFLAGS": true, "CXXFLAGS": true, "CPPFLAGS": true,
		"LDFLAGS": true, "PKG_CONFIG_PATH": true,
		"LANG": true, "LC_ALL": true,
		"SDKROOT": true, "MACOSX_DEPLOYMENT_TARGET": true,
		"DEVELOPER_DIR":     true,
		"SOURCE_DATE_EPOCH": true,
	}

	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if allow[key] {
			env = append(env, kv)
		}
	}

	// Override TMPDIR to keep temp files inside the build directory.
	tmpDir := filepath.Join(cfg.BuildDir, ".grew-tmp")
	os.MkdirAll(tmpDir, 0755)
	env = append(env, "TMPDIR="+tmpDir)

	return env
}
