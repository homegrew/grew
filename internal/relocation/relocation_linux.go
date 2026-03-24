//go:build linux

package relocation

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

func inspectBinary(path string) ([]string, error) {
	patchelf, err := exec.LookPath("patchelf")
	if err != nil {
		return nil, fmt.Errorf("patchelf not found: %w", err)
	}

	var paths []string

	// Collect RPATH/RUNPATH entries.
	if out, err := exec.Command(patchelf, "--print-rpath", "--", path).Output(); err == nil {
		rpath := strings.TrimSpace(string(out))
		if rpath != "" {
			for _, p := range strings.Split(rpath, ":") {
				p = strings.TrimSpace(p)
				if p != "" {
					paths = append(paths, p)
				}
			}
		}
	}

	// Collect DT_NEEDED entries (usually bare names, but can be absolute).
	if out, err := exec.Command(patchelf, "--print-needed", "--", path).Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && strings.HasPrefix(line, "/") {
				paths = append(paths, line)
			}
		}
	}

	return paths, nil
}

func relocateBinary(path, oldPrefix, newPrefix string) error {
	patchelf, err := exec.LookPath("patchelf")
	if err != nil {
		slog.Warn("patchelf not found, skipping relocation")
		return nil
	}

	// Rewrite RPATH/RUNPATH.
	rpathOut, err := exec.Command(patchelf, "--print-rpath", "--", path).Output()
	if err != nil {
		// Not all ELF files have an RPATH; this is not an error.
		return nil
	}

	rpath := strings.TrimSpace(string(rpathOut))
	if rpath == "" || !strings.Contains(rpath, oldPrefix) {
		return nil
	}

	// Replace old prefix in each colon-separated RPATH component.
	components := strings.Split(rpath, ":")
	var newComponents []string
	for _, c := range components {
		newComponents = append(newComponents, strings.Replace(c, oldPrefix, newPrefix, 1))
	}
	newRpath := strings.Join(newComponents, ":")

	slog.Debug(fmt.Sprintf("patchelf --set-rpath %s -- %s", newRpath, path))
	if out, err := exec.Command(patchelf, "--set-rpath", newRpath, "--", path).CombinedOutput(); err != nil {
		return fmt.Errorf("patchelf --set-rpath: %w\n%s", err, string(out))
	}

	return nil
}
