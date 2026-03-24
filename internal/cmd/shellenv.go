package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/homegrew/grew/internal/config"
)

func runShellenv(args []string) error {
	paths := config.Default()
	shell := detectShell(args)

	libPathVar := libraryPathVar()

	switch shell {
	case "fish":
		fmt.Printf("set -gx HOMEGREW_PREFIX %s;\n", shellescape.Quote(paths.Root))
		fmt.Printf("set -gx HOMEGREW_CELLAR %s;\n", shellescape.Quote(paths.Cellar))
		fmt.Printf("set -q PATH; or set PATH ''; set -gx PATH %s $PATH;\n", shellescape.Quote(paths.Bin))
		fmt.Printf("set -q %s; or set %s ''; set -gx %s %s $%s;\n",
			libPathVar, libPathVar, libPathVar, shellescape.Quote(paths.Lib), libPathVar)
	default: // bash, zsh, sh
		fmt.Printf("export HOMEGREW_PREFIX=%s;\n", shellescape.Quote(paths.Root))
		fmt.Printf("export HOMEGREW_CELLAR=%s;\n", shellescape.Quote(paths.Cellar))
		fmt.Printf("export PATH=%s:\"${PATH}\";\n", shellescape.Quote(paths.Bin))
		fmt.Printf("export %s=%s:\"${%s}\";\n", libPathVar, shellescape.Quote(paths.Lib), libPathVar)
	}

	return nil
}

// libraryPathVar returns the environment variable used by the dynamic
// linker to locate shared libraries on the current platform.
//
// On macOS we use DYLD_FALLBACK_LIBRARY_PATH (not DYLD_LIBRARY_PATH)
// so that the system default search paths are still consulted first —
// overriding them can break system frameworks.
//
// On Linux we use LD_LIBRARY_PATH.
func libraryPathVar() string {
	if runtime.GOOS == "darwin" {
		return "DYLD_FALLBACK_LIBRARY_PATH"
	}
	return "LD_LIBRARY_PATH"
}

func detectShell(args []string) string {
	// Explicit argument takes priority: grew shellenv zsh
	if len(args) > 0 {
		return args[0]
	}

	// Check SHELL env var
	shell := filepath.Base(os.Getenv("SHELL"))
	switch {
	case strings.Contains(shell, "fish"):
		return "fish"
	case strings.Contains(shell, "zsh"):
		return "zsh"
	case strings.Contains(shell, "bash"):
		return "bash"
	default:
		return "sh"
	}
}
