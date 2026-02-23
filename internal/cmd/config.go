package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/version"
)

func runConfig(_ []string) error {
	paths := config.Default()

	fmt.Println("HOMEGREW_VERSION:", version.Version())
	fmt.Println("HOMEGREW_PREFIX:", paths.Root)
	fmt.Println("HOMEGREW_CELLAR:", paths.Cellar)
	fmt.Println("HOMEGREW_TAPS:", paths.Taps)
	fmt.Println("HOMEGREW_BIN:", paths.Bin)
	fmt.Println("HOMEGREW_TMP:", paths.Tmp)

	// Core tap
	loader := newLoader(paths.Taps)
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
	out, err := exec.Command(name, flag).Output()
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
