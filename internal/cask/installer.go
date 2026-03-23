package cask

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/pkg/validation"
)

// Installer handles placing cask artifacts into their destinations.
type Installer struct {
	AppDir string // ~/Applications
	BinDir string // ~/.homegrew/bin
}

// InstallApp copies a .app bundle from the staging directory to AppDir.
// It looks for the named .app anywhere inside stageDir (flat or nested).
func (inst *Installer) InstallApp(stageDir, appName string) (string, error) {
	if !strings.HasSuffix(appName, ".app") {
		return "", fmt.Errorf("artifact %q is not a .app bundle", appName)
	}
	if filepath.Base(appName) != appName {
		return "", fmt.Errorf("invalid app name: %q", appName)
	}
	if err := validation.SafePathComponent(appName); err != nil {
		return "", fmt.Errorf("invalid app name %q: %w", appName, err)
	}

	srcApp, err := findApp(stageDir, appName)
	if err != nil {
		return "", err
	}

	// Verify the found app is actually within stageDir (symlink escape protection).
	realSrc, err := filepath.EvalSymlinks(srcApp)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", appName, err)
	}
	absStage, err := filepath.Abs(stageDir)
	if err != nil {
		return "", fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}
	rel, err := filepath.Rel(absStage, realSrc)
	if err != nil {
		return "", fmt.Errorf("resolve relative path from staging directory: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return "", fmt.Errorf("app %s resolves to staging directory itself: %s", appName, realSrc)
	} else if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("app %s resolves outside staging directory: %s", appName, realSrc)
	}

	destApp := filepath.Join(inst.AppDir, appName)

	// Remove existing app if present (reinstall)
	if _, err := os.Stat(destApp); err == nil {
		if err := os.RemoveAll(destApp); err != nil {
			return "", fmt.Errorf("remove existing %s: %w", appName, err)
		}
	}

	if err := fsutil.CopyTree(srcApp, destApp); err != nil {
		return "", fmt.Errorf("copy %s to %s: %w", appName, inst.AppDir, err)
	}

	return destApp, nil
}

// UninstallApp removes a .app bundle from AppDir.
func (inst *Installer) UninstallApp(appName string) error {
	if filepath.Base(appName) != appName {
		return fmt.Errorf("invalid app name: %q", appName)
	}
	if err := validation.SafePathComponent(appName); err != nil {
		return fmt.Errorf("invalid app name %q: %w", appName, err)
	}
	destApp := filepath.Join(inst.AppDir, appName)
	if _, err := os.Stat(destApp); os.IsNotExist(err) {
		return nil // already gone
	}
	return os.RemoveAll(destApp)
}

// LinkBin creates a symlink from BinDir/<name> to the binary at target.
func (inst *Installer) LinkBin(name, target string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid binary name: %q", name)
	}

	// Normalize BinDir and ensure it is an absolute, well-formed directory
	if inst.BinDir == "" {
		return fmt.Errorf("invalid bin directory: empty")
	}
	binDirAbs, err := filepath.Abs(inst.BinDir)
	if err != nil {
		return fmt.Errorf("resolve bin directory: %w", err)
	}
	binDirAbs = filepath.Clean(binDirAbs)

	link := filepath.Join(binDirAbs, name)

	// Safety check: ensure the link path is within the bin directory.
	linkAbs := filepath.Clean(link)

	// Add path separator to avoid prefix tricks (e.g., /tmp/dir vs /tmp/dir2).
	binDirWithSep := binDirAbs
	if !strings.HasSuffix(binDirWithSep, string(os.PathSeparator)) {
		binDirWithSep += string(os.PathSeparator)
	}
	if linkAbs != binDirAbs && !strings.HasPrefix(linkAbs, binDirWithSep) {
		return fmt.Errorf("refusing to create link outside bin directory: %s", linkAbs)
	}

	// Only remove existing path if it is a symlink. Refuse to delete regular files
	// or directories to avoid unintended data loss.
	if info, err := os.Lstat(linkAbs); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat existing link %q: %w", linkAbs, err)
		}
	} else {
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			if err := os.Remove(linkAbs); err != nil {
				return fmt.Errorf("failed to remove existing symlink %q: %w", linkAbs, err)
			}
		} else if mode.IsDir() {
			return fmt.Errorf("refusing to overwrite directory at %q", linkAbs)
		} else if mode.IsRegular() {
			return fmt.Errorf("refusing to overwrite regular file at %q", linkAbs)
		} else {
			return fmt.Errorf("refusing to overwrite non-symlink path at %q (mode %v)", linkAbs, mode)
		}
	}

	// Validate and sanitize the target path for the symlink.
	if target == "" {
		return fmt.Errorf("invalid symlink target: empty")
	}
	cleanTarget := filepath.Clean(target)
	// For relative targets, disallow path traversal via ".." components.
	if !filepath.IsAbs(cleanTarget) {
		for _, part := range strings.Split(cleanTarget, string(os.PathSeparator)) {
			if part == ".." {
				return fmt.Errorf("invalid symlink target contains path traversal: %q", target)
			}
		}
	}

	return os.Symlink(cleanTarget, linkAbs)
}

// UnlinkBin removes a symlink from BinDir.
func (inst *Installer) UnlinkBin(name string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid binary name: %q", name)
	}
	linkPath := filepath.Join(inst.BinDir, name)

	// Ensure we only operate within the configured BinDir.
	binAbs, err := filepath.Abs(inst.BinDir)
	if err != nil {
		return fmt.Errorf("resolve bin dir: %w", err)
	}
	binAbs = filepath.Clean(binAbs)

	linkAbs := filepath.Clean(linkPath)

	binWithSep := binAbs
	if !strings.HasSuffix(binWithSep, string(os.PathSeparator)) {
		binWithSep += string(os.PathSeparator)
	}
	if linkAbs != binAbs && !strings.HasPrefix(linkAbs, binWithSep) {
		return fmt.Errorf("refusing to remove path outside bin directory: %s", linkAbs)
	}

	info, err := os.Lstat(linkAbs)
	if err != nil {
		if os.IsNotExist(err) {
			// already gone
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refusing to remove non-symlink at %q", linkAbs)
	}
	return os.Remove(linkAbs)
}

// findApp searches stageDir for a .app bundle with the given name.
func findApp(stageDir, appName string) (string, error) {
	// Check top level first
	direct := filepath.Join(stageDir, appName)
	if info, err := os.Stat(direct); err == nil && info.IsDir() {
		return direct, nil
	}

	// Walk one level deep
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := validation.SafePathComponent(e.Name()); err != nil {
			continue
		}
		// Prefer a direct subdirectory match first, then look for a nested appName.
		if e.Name() == appName {
			directSub := filepath.Join(stageDir, e.Name())
			return directSub, nil
		}
		nested := filepath.Join(stageDir, e.Name(), appName)
		if info, err := os.Stat(nested); err == nil && info.IsDir() {
			return nested, nil
		}
	}

	return "", fmt.Errorf("could not find %s in extracted archive", appName)
}
