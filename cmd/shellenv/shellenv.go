package shellenv

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/homegrew/grew/pkg/context"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "shellenv [shell]",
	Short: "Print export statements for setting up grew in your shell",
	Long: `Print export statements for setting up grew in your shell. Add the
output to your shell profile to make grew-installed tools available.

Detects the current shell automatically, or specify one explicitly.
Supported shells: bash, zsh, fish, sh.

Setup:
  # bash (~/.bashrc):
  eval "$(grew shellenv)"

  # zsh (~/.zshrc):
  eval "$(grew shellenv)"

  # fish (~/.config/fish/config.fish):
  grew shellenv fish | source`,
	Example: `  grew shellenv
  grew shellenv fish`,
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Debug("starting shellenv command execution")
		ctx, err := context.New()
		if err != nil {
			return err
		}
		paths := ctx.Paths

		// Idempotency guard: mirror Homebrew's early return
		pathEnv := os.Getenv("PATH")
		expectedPrefix := fmt.Sprintf("%s/bin:%s/sbin", paths.Root, paths.Root)
		if strings.HasPrefix(pathEnv, expectedPrefix+":") || pathEnv == expectedPrefix {
			return nil
		}

		shell := detectShell(args)

		root := shellescape.Quote(paths.Root)
		cellar := shellescape.Quote(paths.Cellar)
		repo := shellescape.Quote(paths.GitRepo)

		pathHelperRoot := getPathHelperRoot(paths)

		switch shell {
		case "fish":
			fmt.Printf("set --global --export HOMEGREW_PREFIX %s;\n", root)
			fmt.Printf("set --global --export HOMEGREW_CELLAR %s;\n", cellar)
			fmt.Printf("set --global --export HOMEGREW_REPOSITORY %s;\n", repo)
			fmt.Printf("fish_add_path --global --move --path %s/bin %s/sbin;\n", root, root)
			fmt.Printf("if test -n \"$MANPATH[1]\"; set --global --export MANPATH '' $MANPATH; end;\n")
			fmt.Printf("if not contains %s/share/info $INFOPATH; set --global --export INFOPATH %s/share/info $INFOPATH; end;\n", root, root)

		case "pwsh":
			fmt.Printf("[System.Environment]::SetEnvironmentVariable('HOMEGREW_PREFIX',%s,[System.EnvironmentVariableTarget]::Process)\n", root)
			fmt.Printf("[System.Environment]::SetEnvironmentVariable('HOMEGREW_CELLAR',%s,[System.EnvironmentVariableTarget]::Process)\n", cellar)
			fmt.Printf("[System.Environment]::SetEnvironmentVariable('HOMEGREW_REPOSITORY',%s,[System.EnvironmentVariableTarget]::Process)\n", repo)
			fmt.Printf("[System.Environment]::SetEnvironmentVariable('PATH',$('%s/bin:%s/sbin:'+$ENV:PATH),[System.EnvironmentVariableTarget]::Process)\n", paths.Root, paths.Root)
			fmt.Printf("[System.Environment]::SetEnvironmentVariable('MANPATH',$('%s/share/man'+$(if($ENV:MANPATH){':'+$ENV:MANPATH})+':'),[System.EnvironmentVariableTarget]::Process)\n", paths.Root)
			fmt.Printf("[System.Environment]::SetEnvironmentVariable('INFOPATH',$('%s/share/info'+$(if($ENV:INFOPATH){':'+$ENV:INFOPATH})),[System.EnvironmentVariableTarget]::Process)\n", paths.Root)

		case "csh":
			fmt.Printf("setenv HOMEGREW_PREFIX %s;\n", root)
			fmt.Printf("setenv HOMEGREW_CELLAR %s;\n", cellar)
			fmt.Printf("setenv HOMEGREW_REPOSITORY %s;\n", repo)
			if pathHelperRoot != "" {
				fmt.Printf("eval `/usr/bin/env PATH_HELPER_ROOT=%s /usr/libexec/path_helper -c`;\n", shellescape.Quote(pathHelperRoot))
			} else {
				fmt.Printf("setenv PATH %s/bin:%s/sbin:$PATH;\n", paths.Root, paths.Root)
			}
			fmt.Printf("test ${?MANPATH} -eq 1 && setenv MANPATH :${MANPATH};\n")
			fmt.Printf("setenv INFOPATH %s/share/info`test ${?INFOPATH} -eq 1 && echo :${INFOPATH}`;\n", paths.Root)

		default: // bash, zsh, sh
			fmt.Printf("export HOMEGREW_PREFIX=%s;\n", root)
			fmt.Printf("export HOMEGREW_CELLAR=%s;\n", cellar)
			fmt.Printf("export HOMEGREW_REPOSITORY=%s;\n", repo)
			if shell == "zsh" {
				fmt.Printf("fpath[1,0]=\"%s/share/zsh/site-functions\";\n", paths.Root)
				fmt.Printf("export FPATH;\n")
			}
			if pathHelperRoot != "" {
				fmt.Printf("eval \"$(/usr/bin/env PATH_HELPER_ROOT=%q /usr/libexec/path_helper -s)\"\n", shellescape.Quote(pathHelperRoot))
			} else {
				fmt.Printf("export PATH=\"%s/bin:%s/sbin${PATH+:$PATH}\";\n", paths.Root, paths.Root)
			}
			fmt.Printf("[ -z \"${MANPATH-}\" ] || export MANPATH=\":${MANPATH#:}\";\n")
			fmt.Printf("export INFOPATH=\"%s/share/info:${INFOPATH:-}\";\n", paths.Root)
		}

		return nil
	},
}

func init() {
}

func detectShell(args []string) string {
	// 1. Explicit argument takes priority: grew shellenv zsh
	if len(args) > 0 {
		return strings.TrimPrefix(args[0], "-")
	}

	// 2. parent process check (mirroring brew)
	if name := getParentShellName(); name != "" {
		return name
	}

	// 3. Fall back to SHELL env var
	shell := filepath.Base(os.Getenv("SHELL"))
	switch {
	case strings.Contains(shell, "fish"):
		return "fish"
	case strings.Contains(shell, "zsh"):
		return "zsh"
	case strings.Contains(shell, "bash"):
		return "bash"
	case strings.Contains(shell, "pwsh"):
		return "pwsh"
	case strings.Contains(shell, "csh") || strings.Contains(shell, "tcsh"):
		return "csh"
	default:
		return "sh"
	}
}

func getParentShellName() string {
	ppid := os.Getppid()
	if ppid <= 1 {
		return ""
	}
	// ps -p $PPID -c -o comm=
	// -c is important to get the command name without arguments
	out, err := exec.Command("ps", "-p", strconv.Itoa(ppid), "-c", "-o", "comm=").CombinedOutput()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return ""
	}
	// Trim leading dash which is common for login shells
	return strings.TrimPrefix(filepath.Base(name), "-")
}
