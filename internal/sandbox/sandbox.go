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
// On macOS it uses sandbox-exec (Seatbelt). On other platforms it falls back
// to a clean environment.
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

// ExtractConfig describes the restricted sandbox for archive extraction.
// Extraction gets:
//   - Network access denied
//   - Write access ONLY to the staging directory
//   - Read access to the archive file and system paths
//   - Minimal environment
type ExtractConfig struct {
	ArchiveFile string // path to the downloaded archive (read-only)
	StageDir    string // destination directory (read-write)
}

// ExtractCommand wraps an archive extraction step in platform-specific sandboxing.
// The extraction process can only write to StageDir and read the archive file.
// Network access is denied.
func ExtractCommand(cfg ExtractConfig, name string, args ...string) *exec.Cmd {
	return platformExtractCommand(cfg, name, args...)
}

// IsSandboxed reports whether any form of functional sandboxing (Seatbelt)
// is available and working on the current system.
func IsSandboxed() bool {
	return platformIsSandboxed()
}

// extractEnv returns a minimal environment for extraction.
func extractEnv(cfg ExtractConfig) []string {
	allow := map[string]bool{
		"PATH": true, "HOME": true,
		"LANG": true, "LC_ALL": true,
		"GOCOVERDIR": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if allow[key] {
			env = append(env, kv)
		}
	}
	env = append(env, "TMPDIR="+cfg.StageDir)
	return env
}

// postInstallEnv returns a minimal environment for post-install scripts.
// Only PATH, HOME, LANG, and TMPDIR are passed through. No compiler
// variables, no secrets.
func postInstallEnv(cfg PostInstallConfig) []string {
	allow := map[string]bool{
		"PATH": true, "HOME": true,
		"LANG": true, "LC_ALL": true,
		"GOCOVERDIR": true,
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
		"GOCOVERDIR":        true,
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
