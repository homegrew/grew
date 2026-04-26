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
	// Reject any parent-directory marker anywhere in the component.
	// This is stricter than only rejecting "."/".." and avoids ambiguous inputs.
	if strings.Contains(name, "..") || name == "." {
		return fmt.Errorf("path component contains traversal marker: %q", name)
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
	parts := append([]string{base}, components...)
	joined := filepath.Join(parts...)
	cleaned := filepath.Clean(joined)

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("resolve base path: %w", err)
	}
	absJoined, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve joined path: %w", err)
	}

	// The joined path must be equal to or a child of the base.
	if !IsSubpath(absBase, absJoined) {
		return "", fmt.Errorf("path %q escapes base %q", absJoined, absBase)
	}
	return absJoined, nil
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
