package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Runtime interface {
	RunAsRoot() bool
	DefaultPrefix() string
}

var r Runtime

type runtimeTyp struct {
	prefix string
	isRoot bool
}

func (r *runtimeTyp) DefaultPrefix() string {
	return r.prefix
}

func (r *runtimeTyp) RunAsRoot() bool { return r.isRoot }

func Env() Runtime {
	return r
}

func Init() error {
	ptr, err := new()
	if err != nil {
		return fmt.Errorf("init failed: %w", err)
	}

	r = ptr
	return nil
}

// devModeActive reports whether developer mode is enabled at runtime.
// Requires a devmode build (compile-time).
func devModeActive() bool {
	return DevMode
}

func new() (Runtime, error) {
	if os.Geteuid() == 0 {
		return &runtimeTyp{prefix: SystemPrefix(), isRoot: true}, nil
	}

	if devModeActive() {
		prefix, err := UserPrefix()
		if err != nil {
			return nil, fmt.Errorf("could not initialize the runtime environment: %w", err)
		}
		return &runtimeTyp{prefix: prefix, isRoot: false}, nil
	}

	// Default to system prefix even if not root. Elevation will be
	// requested by specific commands (like setup) if needed.
	return &runtimeTyp{prefix: SystemPrefix(), isRoot: false}, nil
}

// UserPrefix returns the user-local prefix (~/.homegrew).
// Only available in devmode builds. Returns an error if the home
// directory cannot be determined.
func UserPrefix() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine user home directory: %w", err)
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	prefix, err := filepath.Abs(filepath.Join(abs, ".homegrew"))
	if err != nil {
		return "", fmt.Errorf("resolve prefix path: %w", err)
	}
	return prefix, nil
}

// SystemPrefix returns the recommended system-level prefix for the current
// platform. Used by `grew setup` when running with sudo.
//
//   - macOS ARM64 (Apple Silicon): /opt/homegrew
//   - macOS AMD64 (Intel):         /usr/local/homegrew
func SystemPrefix() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "/opt/homegrew"
	}
	return "/usr/local/homegrew"
}
