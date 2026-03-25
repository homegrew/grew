//go:build linux

package relocation

import (
	"fmt"
	"log/slog"
	"os"
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

func applyReplacements(s string, replacements Replacements) (string, bool) {
	for old, new_ := range replacements {
		if strings.Contains(s, old) {
			return strings.Replace(s, old, new_, 1), true
		}
	}
	return s, false
}

func relocateBinary(path string, replacements Replacements) error {
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

	// Apply replacements to each colon-separated RPATH component.
	components := strings.Split(rpath, ":")
	var newComponents []string
	changed := false
	for _, c := range components {
		nc, replaced := applyReplacements(c, replacements)
		if replaced {
			slog.Debug(fmt.Sprintf("relocation: %s: RPATH %s -> %s", relName, c, nc))
			changed = true
		}
		newComponents = append(newComponents, nc)
	}

	if !changed {
		slog.Debug(fmt.Sprintf("relocation: %s: RPATH has no matching replacements, skipping", relName))
		return nil
	}

	newRpath := strings.Join(newComponents, ":")

	slog.Debug(fmt.Sprintf("relocation: patchelf --set-rpath %s %s", newRpath, relName))
	if out, err := exec.Command(patchelf, "--set-rpath", newRpath, "--", path).CombinedOutput(); err != nil {
		return fmt.Errorf("patchelf --set-rpath: %w\n%s", err, string(out))
	}
	slog.Info(fmt.Sprintf("relocated %s", relName))

	return nil
}

// verifyBinary checks that the RPATH/RUNPATH of an ELF binary points to
// existing directories. On Linux, broken rpaths mean the dynamic linker
// won't find shared libraries at runtime.
func verifyBinary(path, prefix string) []Issue {
	patchelf, err := exec.LookPath("patchelf")
	if err != nil {
		return nil
	}

	relName := filepath.Base(path)

	rpathOut, err := exec.Command(patchelf, "--print-rpath", "--", path).Output()
	if err != nil {
		return nil
	}

	rpath := strings.TrimSpace(string(rpathOut))
	if rpath == "" {
		return nil
	}

	var issues []Issue
	for _, component := range strings.Split(rpath, ":") {
		component = strings.TrimSpace(component)
		if component == "" || strings.HasPrefix(component, "$") {
			continue // $ORIGIN etc. are runtime-resolved
		}
		if _, err := os.Stat(component); err != nil {
			slog.Debug(fmt.Sprintf("relocation: verify: %s: RPATH dir missing %s", relName, component))
			issues = append(issues, Issue{Dep: component, Reason: "RPATH directory does not exist"})
		}
	}
	return issues
}
