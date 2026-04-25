//go:build linux

package linkage

import (
	"os/exec"
	"strings"
)

// inspectDeps returns the dynamic library dependencies and RPATH/RUNPATH
// entries for the given ELF binary.
func inspectDeps(path string) (deps []string, rpaths []string, err error) {
	// Try ldd first for dependency resolution.
	lddPath, lddErr := exec.LookPath("ldd")
	if lddErr == nil {
		out, err := exec.Command(lddPath, "--", path).Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// ldd output formats:
				//   libfoo.so => /usr/lib/libfoo.so (0x...)
				//   /lib64/ld-linux-x86-64.so.2 (0x...)
				//   linux-vdso.so.1 (0x...)
				//   libfoo.so => not found
				if strings.Contains(line, "not found") {
					// Extract the soname.
					if idx := strings.Index(line, " =>"); idx > 0 {
						deps = append(deps, strings.TrimSpace(line[:idx]))
					}
					continue
				}
				if idx := strings.Index(line, " => "); idx > 0 {
					resolved := line[idx+4:]
					if paren := strings.Index(resolved, " ("); paren > 0 {
						resolved = resolved[:paren]
					}
					resolved = strings.TrimSpace(resolved)
					if resolved != "" {
						deps = append(deps, resolved)
					}
				} else if strings.HasPrefix(line, "/") {
					// Direct path like /lib64/ld-linux-x86-64.so.2 (0x...)
					if paren := strings.Index(line, " ("); paren > 0 {
						line = line[:paren]
					}
					deps = append(deps, strings.TrimSpace(line))
				}
			}
		}
	}

	// Collect RPATH/RUNPATH via patchelf or readelf.
	if patchelf, err := exec.LookPath("patchelf"); err == nil {
		out, err := exec.Command(patchelf, "--print-rpath", "--", path).Output()
		if err == nil {
			rp := strings.TrimSpace(string(out))
			if rp != "" {
				for _, p := range strings.Split(rp, ":") {
					p = strings.TrimSpace(p)
					if p != "" {
						rpaths = append(rpaths, p)
					}
				}
			}
		}
	} else if readelf, err := exec.LookPath("readelf"); err == nil {
		out, _ := exec.Command(readelf, "-d", "--", path).Output()
		if out != nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "RPATH") || strings.Contains(line, "RUNPATH") {
					// Format: 0x... (RPATH)  Library rpath: [/some/path:/other]
					if idx := strings.Index(line, "["); idx >= 0 {
						if end := strings.Index(line[idx:], "]"); end > 0 {
							rp := line[idx+1 : idx+end]
							for _, p := range strings.Split(rp, ":") {
								p = strings.TrimSpace(p)
								if p != "" {
									rpaths = append(rpaths, p)
								}
							}
						}
					}
				}
			}
		}
	}

	// If ldd wasn't available, fall back to readelf for deps.
	if lddErr != nil && len(deps) == 0 {
		if readelf, err := exec.LookPath("readelf"); err == nil {
			out, _ := exec.Command(readelf, "-d", "--", path).Output()
			if out != nil {
				for _, line := range strings.Split(string(out), "\n") {
					if strings.Contains(line, "NEEDED") {
						// Format: 0x... (NEEDED)  Shared library: [libfoo.so]
						if idx := strings.Index(line, "["); idx >= 0 {
							if end := strings.Index(line[idx:], "]"); end > 0 {
								deps = append(deps, line[idx+1:idx+end])
							}
						}
					}
				}
			}
		}
	}

	return deps, rpaths, nil
}

// isSystemLibPlatform reports whether the path is a Linux system library.
func isSystemLibPlatform(p string) bool {
	systemPrefixes := []string{
		"/usr/lib/",
		"/usr/lib64/",
		"/lib/",
		"/lib64/",
		"/usr/local/lib/",
	}
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	// Virtual DSOs.
	if p == "linux-vdso.so.1" || p == "linux-gate.so.1" {
		return true
	}
	return false
}
