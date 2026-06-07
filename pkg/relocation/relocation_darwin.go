
package relocation

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// applyReplacements returns s with the first matching replacement applied.
// Keys are tried longest-first so the most specific source path wins.
func applyReplacements(s string, replacements Replacements) (string, bool) {
	for _, old := range replacements.OrderedKeys() {
		if strings.Contains(s, old) {
			return strings.Replace(s, old, replacements[old], 1), true
		}
	}
	return s, false
}

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

func relocateBinary(path string, replacements Replacements) error {
	otool, err := exec.LookPath("otool")
	if err != nil {
		return fmt.Errorf("otool not found: %w", err)
	}
	installNameTool, err := exec.LookPath("install_name_tool")
	if err != nil {
		return fmt.Errorf("install_name_tool not found: %w", err)
	}

	relName := filepath.Base(path)
	var args []string

	// Determine install name (LC_ID_DYLIB) via otool -D.
	idOut, _ := exec.Command(otool, "-D", path).Output()
	var installName string
	if idLines := strings.Split(strings.TrimSpace(string(idOut)), "\n"); len(idLines) >= 2 {
		installName = strings.TrimSpace(idLines[1])
	}
	if installName != "" {
		if newID, changed := applyReplacements(installName, replacements); changed {
			slog.Debug(fmt.Sprintf("relocation: %s: -id %s -> %s", relName, installName, newID))
			args = append(args, "-id", newID)
		}
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
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, ":") {
			continue
		}
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		libPath := strings.TrimSpace(line)
		if libPath == "" || libPath == installName {
			continue
		}

		if newPath, changed := applyReplacements(libPath, replacements); changed {
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
					if newRpath, changed := applyReplacements(rpath, replacements); changed {
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

	// Re-sign the binary (required on arm64 macOS).
	codesign, err := exec.LookPath("codesign")
	if err != nil {
		slog.Warn(fmt.Sprintf("relocation: codesign not found, %s signature may be invalid", relName))
		return nil
	}
	slog.Debug(fmt.Sprintf("relocation: codesign --force --sign - %s", relName))
	if out, err := exec.Command(codesign, "--force", "--sign", "-", path).CombinedOutput(); err != nil {
		slog.Warn(fmt.Sprintf("relocation: codesign failed for %s: %v\n%s", relName, err, string(out)))
	}

	return nil
}

// verifyBinary checks that all dynamic library dependencies of path can be
// resolved. On macOS, it runs otool -L and checks that each referenced
// library exists on disk (resolving @rpath via LC_RPATH entries).
func verifyBinary(path, prefix string) []Issue {
	otool, err := exec.LookPath("otool")
	if err != nil {
		return nil
	}

	relName := filepath.Base(path)

	// Get library deps.
	libOut, err := exec.Command(otool, "-L", path).Output()
	if err != nil {
		return nil
	}

	// Get LC_RPATH entries for @rpath resolution.
	var rpaths []string
	if loadOut, err := exec.Command(otool, "-l", path).Output(); err == nil {
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
							rpaths = append(rpaths, rpath)
						}
						break
					}
				}
			}
		}
	}

	var issues []Issue

	// Check LC_RPATH entries point within our prefix.
	for _, rp := range rpaths {
		if strings.HasPrefix(rp, "@") {
			continue // @loader_path etc. are relative, checked later
		}
		if prefix != "" && strings.HasPrefix(rp, "/") && !strings.HasPrefix(rp, prefix+"/") &&
			!strings.HasPrefix(rp, "/usr/lib") && !strings.HasPrefix(rp, "/System/") {
			slog.Debug(fmt.Sprintf("relocation: verify: %s: LC_RPATH %s outside prefix %s", relName, rp, prefix))
			issues = append(issues, Issue{Dep: rp, Reason: fmt.Sprintf("LC_RPATH points to foreign prefix (expected %s)", prefix)})
		}
	}

	for _, line := range strings.Split(string(libOut), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") {
			continue
		}
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		dep := strings.TrimSpace(line)
		if dep == "" {
			continue
		}

		// System libraries — assume they exist.
		if strings.HasPrefix(dep, "/usr/lib/") || strings.HasPrefix(dep, "/System/") {
			continue
		}

		// Absolute path — check it exists AND resolves within grew's prefix.
		// If it resolves to a different prefix (e.g. /opt/homebrew when grew
		// uses /opt/homegrew), relocation was incomplete.
		if strings.HasPrefix(dep, "/") {
			if _, err := os.Stat(dep); err != nil {
				slog.Debug(fmt.Sprintf("relocation: verify: %s: missing dep %s", relName, dep))
				issues = append(issues, Issue{Dep: dep, Reason: "library not found"})
			} else if prefix != "" && !strings.HasPrefix(dep, prefix+"/") {
				slog.Debug(fmt.Sprintf("relocation: verify: %s: dep %s resolves outside prefix %s", relName, dep, prefix))
				issues = append(issues, Issue{Dep: dep, Reason: fmt.Sprintf("references foreign prefix (expected %s)", prefix)})
			}
			continue
		}

		// @rpath reference — resolve against each LC_RPATH.
		if strings.HasPrefix(dep, "@rpath/") {
			libName := dep[len("@rpath/"):]
			found := false
			var resolvedPath string
			for _, rp := range rpaths {
				// Resolve @loader_path in rpath itself.
				resolved := rp
				if strings.HasPrefix(resolved, "@loader_path/") {
					resolved = filepath.Join(filepath.Dir(path), resolved[len("@loader_path/"):])
				}
				candidate := filepath.Join(resolved, libName)
				if _, err := os.Stat(candidate); err == nil {
					found = true
					resolvedPath = candidate
					break
				}
			}
			if !found {
				slog.Debug(fmt.Sprintf("relocation: verify: %s: unresolved @rpath dep %s (rpaths: %v)", relName, dep, rpaths))
				issues = append(issues, Issue{Dep: dep, Reason: fmt.Sprintf("unresolved @rpath (searched %d rpaths)", len(rpaths))})
			} else if prefix != "" && !strings.HasPrefix(resolvedPath, prefix+"/") {
				slog.Debug(fmt.Sprintf("relocation: verify: %s: @rpath dep %s resolved to %s (outside prefix %s)", relName, dep, resolvedPath, prefix))
				issues = append(issues, Issue{Dep: dep, Reason: fmt.Sprintf("resolves to foreign prefix %s (expected %s)", resolvedPath, prefix)})
			}
			continue
		}

		// @loader_path reference — resolve relative to binary.
		if strings.HasPrefix(dep, "@loader_path/") {
			resolved := filepath.Join(filepath.Dir(path), dep[len("@loader_path/"):])
			if _, err := os.Stat(resolved); err != nil {
				slog.Debug(fmt.Sprintf("relocation: verify: %s: missing @loader_path dep %s -> %s", relName, dep, resolved))
				issues = append(issues, Issue{Dep: dep, Reason: "library not found at @loader_path"})
			}
			continue
		}

		// @executable_path reference — can't easily resolve without knowing the executable.
		// Skip for now.
	}

	return issues
}
