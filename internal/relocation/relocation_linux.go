//go:build linux

package relocation

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/safepath"
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
	if out, err := exec.Command(patchelf, "--print-rpath", path).Output(); err == nil {
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

	// Collect interpreter path.
	slog.Debug(fmt.Sprintf("relocation: patchelf --print-interpreter %s", relName))
	if out, err := exec.Command(patchelf, "--print-interpreter", path).Output(); err == nil {
		interp := strings.TrimSpace(string(out))
		if interp != "" {
			slog.Debug(fmt.Sprintf("relocation: %s: interpreter %s", relName, interp))
			paths = append(paths, interp)
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

	changed := false
	var args []string

	// Rewrite interpreter.
	slog.Debug(fmt.Sprintf("relocation: patchelf --print-interpreter %s", relName))
	if interpOut, err := exec.Command(patchelf, "--print-interpreter", path).Output(); err == nil {
		interp := strings.TrimSpace(string(interpOut))
		if interp != "" {
			if newInterp, replaced := applyReplacements(interp, replacements); replaced {
				slog.Debug(fmt.Sprintf("relocation: %s: interpreter %s -> %s", relName, interp, newInterp))

				// Ensure interpreter paths are safe absolute paths before using them.
				if err := safepath.SafeAbsolutePath(newInterp); err != nil {
					slog.Warn(fmt.Sprintf("relocation: %s: skipping unsafe relocated interpreter %q: %v", relName, newInterp, err))
					goto skipInterpreterRewrite
				}

				// Verify the new interpreter exists. If it doesn't, the binary will
				// fail to execute with "required file not found".
				if _, err := os.Stat(newInterp); err != nil {
					slog.Warn(fmt.Sprintf("relocation: %s: relocated interpreter %s does not exist", relName, newInterp))
					// Fallback: Homebrew Linux bottles depend on their own glibc (ld.so).
					// If it's missing, fall back to the standard x86_64 system interpreter
					// so the binary can at least attempt to run natively.
					systemInterp := "/lib64/ld-linux-x86-64.so.2"
					if _, err := os.Stat(systemInterp); err == nil {
						slog.Info(fmt.Sprintf("relocation: %s: falling back to system interpreter %s", relName, systemInterp))
						newInterp = systemInterp
					}
				}

				args = append(args, "--set-interpreter", newInterp)
				changed = true
			}
		}
	skipInterpreterRewrite:
	}

	// Rewrite RPATH/RUNPATH.
	slog.Debug(fmt.Sprintf("relocation: patchelf --print-rpath %s", relName))
	rpathOut, err := exec.Command(patchelf, "--print-rpath", path).Output()
	if err == nil {
		rpath := strings.TrimSpace(string(rpathOut))
		if rpath != "" {
			// Apply replacements to each colon-separated RPATH component.
			components := strings.Split(rpath, ":")
			var newComponents []string
			rpathChanged := false
			for _, c := range components {
				nc, replaced := applyReplacements(c, replacements)
				if replaced {
					slog.Debug(fmt.Sprintf("relocation: %s: RPATH %s -> %s", relName, c, nc))
					rpathChanged = true
				}
				newComponents = append(newComponents, nc)
			}

			if rpathChanged {
				newRpath := strings.Join(newComponents, ":")
				args = append(args, "--set-rpath", newRpath)
				changed = true
			}
		}
	}

	if !changed {
		slog.Debug(fmt.Sprintf("relocation: %s: no matching replacements, skipping", relName))
		return nil
	}

	args = append(args, path)
	slog.Debug(fmt.Sprintf("relocation: patchelf %s", strings.Join(args, " ")))

	// patchelf modifies the binary in place. Ensure it is writable.
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	originalMode := info.Mode()
	if originalMode&0200 == 0 {
		if err := os.Chmod(path, originalMode|0200); err != nil {
			return fmt.Errorf("make writable: %w", err)
		}
		defer os.Chmod(path, originalMode)
	}

	if out, err := exec.Command(patchelf, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("patchelf: %w\n%s", err, string(out))
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
