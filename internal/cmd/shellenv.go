package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/homegrew/grew/internal/config"
)

func runShellenv(args []string) error {
	slog.Debug("starting shellenv command execution")
	slog.Debug("starting shellenv command execution")
	paths := config.Default()
	shell := detectShell(args)

	root := shellescape.Quote(paths.Root)
	cellar := shellescape.Quote(paths.Cellar)
	bin := shellescape.Quote(paths.Bin)
	man := shellescape.Quote(filepath.Join(paths.Share, "man"))
	info := shellescape.Quote(filepath.Join(paths.Share, "info"))

	switch shell {
	case "fish":
		fmt.Printf("set -gx HOMEGREW_PREFIX %s;\n", root)
		fmt.Printf("set -gx HOMEGREW_CELLAR %s;\n", cellar)
		fmt.Printf("set -gx HOMEGREW_REPOSITORY %s;\n", root)
		fmt.Printf("set -q PATH; or set PATH ''; set -gx PATH %s $PATH;\n", bin)
		fmt.Printf("set -q MANPATH; or set MANPATH ''; set -gx MANPATH %s $MANPATH;\n", man)
		fmt.Printf("set -q INFOPATH; or set INFOPATH ''; set -gx INFOPATH %s $INFOPATH;\n", info)
	default: // bash, zsh, sh
		fmt.Printf("export HOMEGREW_PREFIX=%s;\n", root)
		fmt.Printf("export HOMEGREW_CELLAR=%s;\n", cellar)
		fmt.Printf("export HOMEGREW_REPOSITORY=%s;\n", root)
		fmt.Printf("export PATH=%s:\"${PATH}\";\n", bin)
		fmt.Printf("export MANPATH=%s:\"${MANPATH:-}\";\n", man)
		fmt.Printf("export INFOPATH=%s:\"${INFOPATH:-}\";\n", info)
		if shell == "zsh" {
			fmt.Printf("fpath=(%s ${fpath});\n", shellescape.Quote(filepath.Join(paths.Share, "zsh", "site-functions")))
		}
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
