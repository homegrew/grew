package config

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/context"
	"github.com/homegrew/grew/internal/version"
	"github.com/spf13/cobra"
)

var Command = &cobra.Command{
	Use:   "config",
	Short: "Show grew and system configuration",
	Long: `Show grew and system configuration including paths, installed
package count, Go version, OS, CPU, and detected tools (git, curl,
clang). Also shows any HOMEGREW_* environment variables.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Debug("starting config command execution")
		ctx, err := context.New()
		if err != nil {
			return err
		}
		paths := ctx.Paths

		fmt.Println("HOMEGREW_VERSION:", version.Version())
		fmt.Println("HOMEGREW_PREFIX:", paths.Root)
		fmt.Println("HOMEGREW_CELLAR:", paths.Cellar)
		fmt.Println("HOMEGREW_TAPS:", paths.Taps)
		fmt.Println("HOMEGREW_BIN:", paths.Bin)
		fmt.Println("HOMEGREW_TMP:", paths.Tmp)

		// Core tap
		loader := context.NewLoader(paths.Taps)
		all, _ := loader.LoadAll()
		fmt.Printf("Core tap formulas: %d\n", len(all))

		// Installed packages
		cel := &cellar.Cellar{Path: paths.Cellar}
		installed, _ := cel.List()
		fmt.Printf("Installed packages: %d\n", len(installed))

		// System
		fmt.Println()
		fmt.Printf("Go: %s\n", runtime.Version())
		fmt.Printf("OS: %s\n", osInfo())
		fmt.Printf("CPU: %s (%d cores)\n", runtime.GOARCH, runtime.NumCPU())
		fmt.Printf("Git: %s\n", toolVersion("git", "--version"))
		fmt.Printf("Curl: %s\n", toolVersion("curl", "--version"))
		fmt.Printf("Clang: %s\n", toolVersion("clang", "--version"))
		fmt.Printf("Shell: %s\n", os.Getenv("SHELL"))

		// HOMEGREW_* env vars
		envVars := grewEnvVars()
		if len(envVars) > 0 {
			fmt.Println()
			for _, kv := range envVars {
				fmt.Println(kv)
			}
		}

		return nil
	},
}

func init() {
}

func osInfo() string {
	out, err := exec.Command("uname", "-srm").Output()
	if err != nil {
		return runtime.GOOS + " " + runtime.GOARCH
	}
	return strings.TrimSpace(string(out))
}

func toolVersion(name string, flag string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return "N/A"
	}
	out, err := exec.Command(path, flag).Output()
	if err != nil {
		return path
	}
	// Take just the first line
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return line + " => " + path
}

func grewEnvVars() []string {
	var vars []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "HOMEGREW_") {
			vars = append(vars, env)
		}
	}
	return vars
}
