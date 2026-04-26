// Package safepath provides utilities for safe filesystem path manipulation,
// ensuring that paths remain within expected boundaries and are well-formed.
package safepath

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// IsSubpath checks if target is a subpath of base. Both paths are evaluated
// as absolute paths. It returns true if target is equal to base or is a
// descendant of base.
func IsSubpath(base, target string) bool {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absBase = filepath.Clean(absBase)
	absTarget = filepath.Clean(absTarget)

	if runtime.GOOS == "windows" {
		absBase = strings.ToLower(absBase)
		absTarget = strings.ToLower(absTarget)
	}

	if absTarget == absBase {
		return true
	}
	baseWithSep := absBase
	if !strings.HasSuffix(baseWithSep, string(filepath.Separator)) {
		baseWithSep += string(filepath.Separator)
	}
	return strings.HasPrefix(absTarget, baseWithSep)
}

// CheckSubpath returns an error if target is not a subpath of base.
// A path is considered a subpath if it is equal to or a descendant of base.
func CheckSubpath(base, target string) error {
	if !IsSubpath(base, target) {
		return fmt.Errorf("path %q escapes base directory %q", target, base)
	}
	return nil
}

// SafePathComponent checks that a filename component does not contain
// path separators, ".." traversals, or null bytes. Use this to validate
// any user-supplied string before joining it into a filesystem path.
func SafePathComponent(name string) error {
	if name == "" {
		return fmt.Errorf("empty path component")
	}
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("path contains null byte")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("path component contains separator: %q", name)
	}
	if name == ".." || name == "." {
		return fmt.Errorf("path component is a traversal: %q", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("path component must be a single element: %q", name)
	}
	return nil
}

// SafeJoin joins base and child path components, then verifies the result
// resolves within base. Returns the cleaned joined path or an error if the
// result would escape base (e.g. via ".." traversal or symlinks).
func SafeJoin(base string, components ...string) (string, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve base path: %w", err)
	}
	absBase = filepath.Clean(absBase)

	safeComponents := make([]string, 0, len(components))
	for _, c := range components {
		if c == "" {
			continue
		}
		if strings.Contains(c, "\x00") {
			return "", fmt.Errorf("path contains null byte")
		}
		if filepath.IsAbs(c) {
			return "", fmt.Errorf("absolute component is not allowed: %q", c)
		}
		clean := filepath.Clean(c)
		if clean == "." || clean == ".." {
			return "", fmt.Errorf("unsafe path component: %q", c)
		}
		if strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path traversal detected: %q", c)
		}
		safeComponents = append(safeComponents, clean)
	}

	parts := append([]string{absBase}, safeComponents...)
	joined := filepath.Clean(filepath.Join(parts...))

	// Resolve symlinks for the parent path so sink-time operations cannot escape
	// base via symlinked directories.
	parent := filepath.Dir(joined)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		// If parent does not exist yet, resolve as much as possible from base.
		resolvedParent = parent
	}
	finalTarget := filepath.Clean(filepath.Join(resolvedParent, filepath.Base(joined)))

	// The final path must be equal to or a child of the base.
	if !IsSubpath(absBase, finalTarget) {
		return "", fmt.Errorf("path %q escapes base %q", finalTarget, absBase)
	}
	return finalTarget, nil
}

// SafeAbsolutePath validates that path is an absolute, clean path with no
// traversal elements. It rejects empty strings, relative paths, and paths
// that differ from their filepath.Clean form (e.g. contain "..", trailing
// slashes, or redundant separators). The root path "/" is rejected because
// it is never a valid target for file operations in this project.
func SafeAbsolutePath(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("path must be absolute: %q", path)
	}
	if clean == string(filepath.Separator) {
		return fmt.Errorf("path must not be the filesystem root")
	}
	if clean != path {
		return fmt.Errorf("path contains traversal or redundant elements: %q (cleaned to %q)", path, clean)
	}
	return nil
}
