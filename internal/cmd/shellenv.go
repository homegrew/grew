package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/config"
)

func runShellenv(args []string) error {
	paths := config.Default()
	shell := detectShell(args)

	switch shell {
	case "fish":
		fmt.Printf("set -gx HOMEGREW_PREFIX \"%s\";\n", paths.Root)
		fmt.Printf("set -gx HOMEGREW_CELLAR \"%s\";\n", paths.Cellar)
		fmt.Printf("set -q PATH; or set PATH ''; set -gx PATH \"%s\" $PATH;\n", paths.Bin)
	default: // bash, zsh, sh
		fmt.Printf("export HOMEGREW_PREFIX=\"%s\";\n", paths.Root)
		fmt.Printf("export HOMEGREW_CELLAR=\"%s\";\n", paths.Cellar)
		fmt.Printf("export PATH=\"%s:${PATH}\";\n", paths.Bin)
	}

	return nil
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
