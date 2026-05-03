
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/homegrew/grew/internal/config"
)

func getPathHelperRoot(paths config.Paths) string {
	if !isMacOSVersionAtLeast(14, 0) {
		return ""
	}

	if _, err := os.Stat("/usr/libexec/path_helper"); err != nil {
		return ""
	}

	pathsFile := filepath.Join(paths.Root, "etc", "paths")
	if _, err := os.Stat(pathsFile); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(pathsFile), 0755)
		content := fmt.Sprintf("%s/bin\n%s/sbin\n", paths.Root, paths.Root)
		_ = os.WriteFile(pathsFile, []byte(content), 0644)
	}

	if _, err := os.Stat(pathsFile); err == nil {
		return paths.Root
	}

	return ""
}

func isMacOSVersionAtLeast(major, minor int) bool {
	// sw_vers -productVersion
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return false
	}
	version := strings.TrimSpace(string(out))
	parts := strings.Split(version, ".")
	if len(parts) < 1 {
		return false
	}
	vMajor, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	if vMajor > major {
		return true
	}
	if vMajor < major {
		return false
	}
	if len(parts) < 2 {
		return minor == 0
	}
	vMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return vMinor >= minor
}
