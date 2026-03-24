//go:build linux

package relocation

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
)

func inspectBinary(path string) ([]string, error) {
	patchelf, err := exec.LookPath("patchelf")
	if err != nil {
		return nil, fmt.Errorf("patchelf not found: %w", err)
	}

	relName := filepath.Base(path)
	var paths []string

	// Collect RPATH/RUNPATH entries.
	slog.Debug(fmt.Sprintf("relocation: patchelf --print-rpath %s", relName))
	if out, err := exec.Command(patchelf, "--print-rpath", "--", path).Output(); err == nil {
		rpath := strings.TrimSpace(string(out))
		if rpath != "" {
			for _, p := range strings.Split(rpath, ":") {
				p = strings.TrimSpace(p)
				if p != "" {
					slog.Debug(fmt.Sprintf("relocation: %s: RPATH component %s", relName, p))
					paths = append(paths, p)
				}
			}
		}
	}

	return paths, nil
}

func relocateBinary(path, oldPrefix, newPrefix string) error {
	patchelf, err := exec.LookPath("patchelf")
	if err != nil {
		slog.Warn("relocation: patchelf not found, skipping")
		return nil
	}

	relName := filepath.Base(path)

	// Rewrite RPATH/RUNPATH.
	slog.Debug(fmt.Sprintf("relocation: patchelf --print-rpath %s", relName))
	rpathOut, err := exec.Command(patchelf, "--print-rpath", "--", path).Output()
	if err != nil {
		slog.Debug(fmt.Sprintf("relocation: %s: no RPATH (not an error)", relName))
		return nil
	}

	rpath := strings.TrimSpace(string(rpathOut))
	if rpath == "" {
		slog.Debug(fmt.Sprintf("relocation: %s: empty RPATH, nothing to do", relName))
		return nil
	}
	if !strings.Contains(rpath, oldPrefix) {
		slog.Debug(fmt.Sprintf("relocation: %s: RPATH does not contain old prefix, skipping", relName))
		return nil
	}

	// Replace old prefix in each colon-separated RPATH component.
	components := strings.Split(rpath, ":")
	var newComponents []string
	for _, c := range components {
		nc := strings.Replace(c, oldPrefix, newPrefix, 1)
		if nc != c {
			slog.Debug(fmt.Sprintf("relocation: %s: RPATH %s -> %s", relName, c, nc))
		}
		newComponents = append(newComponents, nc)
	}
	newRpath := strings.Join(newComponents, ":")

	slog.Debug(fmt.Sprintf("relocation: patchelf --set-rpath %s %s", newRpath, relName))
	if out, err := exec.Command(patchelf, "--set-rpath", newRpath, "--", path).CombinedOutput(); err != nil {
		return fmt.Errorf("patchelf --set-rpath: %w\n%s", err, string(out))
	}
	slog.Info(fmt.Sprintf("relocated %s", relName))

	return nil
}
