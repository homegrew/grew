//go:build darwin

package relocation

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

func inspectBinary(path string) ([]string, error) {
	otool, err := exec.LookPath("otool")
	if err != nil {
		return nil, fmt.Errorf("otool not found: %w", err)
	}

	out, err := exec.Command(otool, "-L", "--", path).Output()
	if err != nil {
		return nil, fmt.Errorf("otool -L %s: %w", path, err)
	}

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "/") {
			continue
		}
		// Format: /path/to/lib.dylib (compatibility version ...)
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		paths = append(paths, strings.TrimSpace(line))
	}
	return paths, nil
}

func relocateBinary(path, oldPrefix, newPrefix string) error {
	otool, err := exec.LookPath("otool")
	if err != nil {
		slog.Warn("otool not found, skipping relocation")
		return nil
	}
	int, err := exec.LookPath("install_name_tool")
	if err != nil {
		slog.Warn("install_name_tool not found, skipping relocation")
		return nil
	}

	// Collect library load commands.
	libOut, err := exec.Command(otool, "-L", "--", path).Output()
	if err != nil {
		return fmt.Errorf("otool -L: %w", err)
	}

	// Collect LC_RPATH entries.
	loadOut, err := exec.Command(otool, "-l", "--", path).Output()
	if err != nil {
		return fmt.Errorf("otool -l: %w", err)
	}

	var args []string
	firstLib := true

	// Process library paths from otool -L output.
	for _, line := range strings.Split(string(libOut), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "/") {
			continue
		}
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		libPath := strings.TrimSpace(line)

		if !strings.Contains(libPath, oldPrefix) {
			firstLib = false
			continue
		}

		newPath := strings.Replace(libPath, oldPrefix, newPrefix, 1)
		if firstLib {
			// First library is the binary's own install name (LC_ID_DYLIB).
			args = append(args, "-id", newPath)
		} else {
			args = append(args, "-change", libPath, newPath)
		}
		firstLib = false
	}

	// Process LC_RPATH entries from otool -l output.
	lines := strings.Split(string(loadOut), "\n")
	for i, line := range lines {
		if strings.Contains(line, "cmd LC_RPATH") {
			// The path is typically 2 lines after "cmd LC_RPATH":
			// "cmd LC_RPATH" → "cmdsize ..." → "path /some/path (offset ...)"
			for j := i + 1; j < len(lines) && j <= i+3; j++ {
				trimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(trimmed, "path ") {
					rpath := trimmed[len("path "):]
					if idx := strings.Index(rpath, " (offset"); idx != -1 {
						rpath = rpath[:idx]
					}
					rpath = strings.TrimSpace(rpath)
					if strings.Contains(rpath, oldPrefix) {
						newRpath := strings.Replace(rpath, oldPrefix, newPrefix, 1)
						args = append(args, "-rpath", rpath, newRpath)
					}
					break
				}
			}
		}
	}

	if len(args) == 0 {
		return nil // nothing to relocate in this binary
	}

	// Run install_name_tool with all accumulated changes.
	args = append(args, "--", path)
	slog.Debug(fmt.Sprintf("install_name_tool %s", strings.Join(args, " ")))
	if out, err := exec.Command(int, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("install_name_tool: %w\n%s", err, string(out))
	}

	// Re-sign the binary (required on arm64 macOS, harmless on x86_64).
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		slog.Warn("codesign not found, binary signature may be invalid")
		return nil
	}
	if out, err := exec.Command(codesign, "--force", "--sign", "-", "--", path).CombinedOutput(); err != nil {
		slog.Warn(fmt.Sprintf("codesign failed for %s: %v\n%s", path, err, string(out)))
	}

	return nil
}
