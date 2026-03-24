//go:build darwin

package relocation

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
)

func inspectBinary(path string) ([]string, error) {
	otool, err := exec.LookPath("otool")
	if err != nil {
		return nil, fmt.Errorf("otool not found: %w", err)
	}

	relName := filepath.Base(path)
	var paths []string

	// Capture the install name (LC_ID_DYLIB) — present only for dylibs.
	// This is the single most reliable source of the build prefix, since
	// it contains the full absolute path the dylib was built with.
	slog.Debug(fmt.Sprintf("relocation: otool -D %s", relName))
	if idOut, err := exec.Command(otool, "-D", path).Output(); err == nil {
		if idLines := strings.Split(strings.TrimSpace(string(idOut)), "\n"); len(idLines) >= 2 {
			installName := strings.TrimSpace(idLines[1])
			if installName != "" {
				slog.Debug(fmt.Sprintf("relocation: %s: LC_ID_DYLIB %s", relName, installName))
				paths = append(paths, installName)
			}
		}
	}

	// Capture library dependencies from otool -L.
	slog.Debug(fmt.Sprintf("relocation: otool -L %s", relName))
	out, err := exec.Command(otool, "-L", path).Output()
	if err != nil {
		return nil, fmt.Errorf("otool -L %s: %w", path, err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the header line: "<binary>:"
		if strings.HasSuffix(line, ":") {
			continue
		}
		// Strip " (compatibility version ...)" suffix.
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		dep := strings.TrimSpace(line)
		if dep == "" {
			continue
		}
		// Capture both absolute paths and @rpath/@loader_path/@executable_path refs.
		if strings.HasPrefix(dep, "/") || strings.HasPrefix(dep, "@") {
			slog.Debug(fmt.Sprintf("relocation: %s: dep %s", relName, dep))
			paths = append(paths, dep)
		}
	}

	// Collect LC_RPATH entries from otool -l output.
	// These contain the absolute prefix paths that @rpath references resolve against.
	slog.Debug(fmt.Sprintf("relocation: otool -l %s", relName))
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
							slog.Debug(fmt.Sprintf("relocation: %s: LC_RPATH %s", relName, rpath))
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
		slog.Warn("relocation: otool not found, skipping")
		return nil
	}
	installNameTool, err := exec.LookPath("install_name_tool")
	if err != nil {
		slog.Warn("relocation: install_name_tool not found, skipping")
		return nil
	}

	relName := filepath.Base(path)
	var args []string

	// Determine install name (LC_ID_DYLIB) via otool -D; present only for dylibs.
	idOut, _ := exec.Command(otool, "-D", path).Output()
	var installName string
	if idLines := strings.Split(strings.TrimSpace(string(idOut)), "\n"); len(idLines) >= 2 {
		installName = strings.TrimSpace(idLines[1])
	}
	if installName != "" && strings.Contains(installName, oldPrefix) {
		newID := strings.Replace(installName, oldPrefix, newPrefix, 1)
		slog.Debug(fmt.Sprintf("relocation: %s: -id %s -> %s", relName, installName, newID))
		args = append(args, "-id", newID)
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

		if libPath == installName {
			continue
		}

		if strings.Contains(libPath, oldPrefix) {
			newPath := strings.Replace(libPath, oldPrefix, newPrefix, 1)
			slog.Debug(fmt.Sprintf("relocation: %s: -change %s -> %s", relName, libPath, newPath))
			args = append(args, "-change", libPath, newPath)
		}
	}

	// Process LC_RPATH entries from otool -l output.
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
					if strings.Contains(rpath, oldPrefix) {
						newRpath := strings.Replace(rpath, oldPrefix, newPrefix, 1)
						slog.Debug(fmt.Sprintf("relocation: %s: -rpath %s -> %s", relName, rpath, newRpath))
						args = append(args, "-rpath", rpath, newRpath)
					}
					break
				}
			}
		}
	}

	if len(args) == 0 {
		slog.Debug(fmt.Sprintf("relocation: %s: no paths to rewrite", relName))
		return nil
	}

	// Run install_name_tool with all accumulated changes.
	args = append(args, path)
	slog.Debug(fmt.Sprintf("relocation: install_name_tool %s", strings.Join(args, " ")))
	if out, err := exec.Command(installNameTool, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("install_name_tool: %w\n%s", err, string(out))
	}
	slog.Info(fmt.Sprintf("relocated %s", relName))

	// Re-sign the binary (required on arm64 macOS, harmless on x86_64).
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		slog.Warn(fmt.Sprintf("relocation: codesign not found, %s signature may be invalid", relName))
		return nil
	}
	slog.Debug(fmt.Sprintf("relocation: codesign --force --sign - %s", relName))
	if out, err := exec.Command(codesign, "--force", "--sign", "-", "--", path).CombinedOutput(); err != nil {
		slog.Warn(fmt.Sprintf("relocation: codesign failed for %s (binary may not execute on Apple Silicon): %v\n%s", relName, err, string(out)))
	}

	return nil
}
