
package linkage

import (
	"fmt"
	"os/exec"
	"strings"
)

// inspectDeps returns the dynamic library dependencies and LC_RPATH entries
// for the given Mach-O binary.
func inspectDeps(path string) (deps []string, rpaths []string, err error) {
	otool, err := exec.LookPath("otool")
	if err != nil {
		return nil, nil, fmt.Errorf("otool not found: %w", err)
	}

	// Library dependencies from otool -L.
	out, err := exec.Command(otool, "-L", "--", path).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("otool -L %s: %w", path, err)
	}

	first := true
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the header line: "<binary>:" (ends with ":")
		if strings.HasSuffix(line, ":") {
			continue
		}
		// Format: /path/to/lib.dylib (compatibility version ...)
		// or:     @rpath/lib.dylib (compatibility version ...)
		if idx := strings.Index(line, " ("); idx != -1 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// For dylibs, the first entry in otool -L after the header is
		// the library's own install name (LC_ID_DYLIB). Skip it.
		if first {
			first = false
			// Detect if this is a dylib by checking otool -D.
			idOut, _ := exec.Command(otool, "-D", "--", path).Output()
			if idLines := strings.Split(strings.TrimSpace(string(idOut)), "\n"); len(idLines) >= 2 {
				installName := strings.TrimSpace(idLines[1])
				if installName == line {
					continue
				}
			}
		}

		deps = append(deps, line)
	}

	// LC_RPATH entries from otool -l.
	loadOut, err := exec.Command(otool, "-l", "--", path).Output()
	if err != nil {
		return deps, nil, nil
	}

	lines := strings.Split(string(loadOut), "\n")
	for i, line := range lines {
		if strings.Contains(line, "cmd LC_RPATH") {
			for j := i + 1; j < len(lines) && j <= i+3; j++ {
				trimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(trimmed, "path ") {
					rp := trimmed[len("path "):]
					if idx := strings.Index(rp, " (offset"); idx != -1 {
						rp = rp[:idx]
					}
					rp = strings.TrimSpace(rp)
					if rp != "" {
						rpaths = append(rpaths, rp)
					}
					break
				}
			}
		}
	}

	return deps, rpaths, nil
}

// isSystemLibPlatform reports whether the path is a macOS system library.
func isSystemLibPlatform(p string) bool {
	systemPrefixes := []string{
		"/usr/lib/",
		"/System/Library/",
		"/Library/Apple/",
		"/usr/local/lib/system/",
	}
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	// Exact match for top-level system libs (rare but possible).
	return p == "/usr/lib/libSystem.B.dylib"
}
