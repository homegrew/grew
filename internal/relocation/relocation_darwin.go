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

	out, err := exec.Command(otool, "-L", path).Output()
	if err != nil {
		return nil, fmt.Errorf("otool -L %s: %w", path, err)
	}

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "/") {
			continue
		}
		// Skip the header line emitted by otool -L: "<binary>:" (ends with ":").
		if strings.HasSuffix(line, ":") {
			continue
		}
		// Format: /path/to/lib.dylib (compatibility version ...)
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		paths = append(paths, strings.TrimSpace(line))
	}

	// Also collect LC_RPATH entries from otool -l output.
	// Binaries using @rpath-based deps may have no absolute paths in
	// otool -L, but their rpaths contain the old prefix.
	loadOut, _ := exec.Command(otool, "-l", path).Output()
	if loadOut != nil {
		lines := strings.Split(string(loadOut), "\n")
		for i, line := range lines {
			if strings.Contains(line, "cmd LC_RPATH") {
				for j := i + 1; j < len(lines) && j <= i+3; j++ {
					trimmed := strings.TrimSpace(lines[j])
					if strings.HasPrefix(trimmed, "path ") {
						rpath := trimmed[len("path "):]
						if idx := strings.Index(rpath, " (offset"); idx != -1 {
							rpath = rpath[:idx]
						}
						rpath = strings.TrimSpace(rpath)
						if rpath != "" {
							paths = append(paths, rpath)
						}
						break
					}
				}
			}
		}
	}

	return paths, nil
}

func relocateBinary(path, oldPrefix, newPrefix string) error {
	otool, err := exec.LookPath("otool")
	if err != nil {
		slog.Warn("otool not found, skipping relocation")
		return nil
	}
	installNameTool, err := exec.LookPath("install_name_tool")
	if err != nil {
		slog.Warn("install_name_tool not found, skipping relocation")
		return nil
	}

	var args []string

	// Determine install name (LC_ID_DYLIB) via otool -D; present only for dylibs.
	idOut, _ := exec.Command(otool, "-D", path).Output()
	var installName string
	if idLines := strings.Split(strings.TrimSpace(string(idOut)), "\n"); len(idLines) >= 2 {
		installName = strings.TrimSpace(idLines[1])
	}
	if installName != "" && strings.Contains(installName, oldPrefix) {
		args = append(args, "-id", strings.Replace(installName, oldPrefix, newPrefix, 1))
	}

	// Collect library load commands via otool -L.
	libOut, err := exec.Command(otool, "-L", path).Output()
	if err != nil {
		return fmt.Errorf("otool -L: %w", err)
	}

	// Collect LC_RPATH entries.
	loadOut, err := exec.Command(otool, "-l", path).Output()
	if err != nil {
		return fmt.Errorf("otool -l: %w", err)
	}

	// Process library paths from otool -L output.
	// The first line is "<binary>:" (the file header) – skip it.
	// For dylibs the first dependency entry is the install name (LC_ID_DYLIB),
	// already handled above via -id; skip it to avoid a conflicting -change.
	libLines := strings.Split(string(libOut), "\n")
	for _, line := range libLines[1:] {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "/") {
			continue
		}
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		libPath := strings.TrimSpace(line)

		// Skip the install name; it is handled via -id above.
		if libPath == installName {
			continue
		}

		if strings.Contains(libPath, oldPrefix) {
			args = append(args, "-change", libPath, strings.Replace(libPath, oldPrefix, newPrefix, 1))
		}
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
	args = append(args, path)
	slog.Debug(fmt.Sprintf("install_name_tool %s", strings.Join(args, " ")))
	if out, err := exec.Command(installNameTool, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("install_name_tool: %w\n%s", err, string(out))
	}

	// Re-sign the binary (required on arm64 macOS, harmless on x86_64).
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		slog.Warn("codesign not found, binary signature may be invalid")
		return nil
	}
	if out, err := exec.Command(codesign, "--force", "--sign", "-", "--", path).CombinedOutput(); err != nil {
		slog.Warn(fmt.Sprintf("codesign failed for %s (binary may not execute on Apple Silicon): %v\n%s", path, err, string(out)))
	}

	return nil
}
