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

func new() (Runtime, error) {
	isRoot := os.Geteuid() == 0
	if isRoot {
		return &runtimeTyp{prefix: SystemPrefix(), isRoot: isRoot}, nil
	}

	prefix, err := UserPrefix()
	if err != nil {
		return nil, fmt.Errorf("could not initialize the runtime environment: %w", err)
	}

	return &runtimeTyp{prefix: prefix, isRoot: isRoot}, nil
}

// SystemPrefix returns the recommended system-level prefix for the current
// platform. Used by `grew setup` when running with sudo.
//
//   - macOS ARM64 (Apple Silicon): /opt/homegrew
//   - macOS AMD64 (Intel):         /usr/local/homegrew
//   - Linux:                        /usr/local/homegrew
func SystemPrefix() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "/opt/homegrew"
	}
	return "/usr/local/homegrew"
}

// UserPrefix returns the user-local prefix (~/.homegrew).
// Returns an error if the home directory cannot be determined.
func UserPrefix() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine user home directory: %w", err)
	}

	abs, err1 := filepath.Abs(home)
	if err1 != nil {
		return "", fmt.Errorf("could not determine absolute path of user home directory: %w", err)
	}

	prefix, err2 := filepath.Abs(filepath.Join(abs, ".homegrew"))
	if err2 != nil {
		return "", fmt.Errorf("could not determine absolute path of user home directory: %w", err)
	}

	return prefix, nil
}
