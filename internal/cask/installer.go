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
	if !strings.HasPrefix(realSrc, absStage+string(filepath.Separator)) && realSrc != absStage {
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
	linkAbs, err := filepath.Abs(link)
	if err != nil {
		return fmt.Errorf("resolve link path: %w", err)
	}
	linkAbs = filepath.Clean(linkAbs)

	// Add path separator to avoid prefix tricks (e.g., /tmp/dir vs /tmp/dir2).
	binDirWithSep := binDirAbs
	if !strings.HasSuffix(binDirWithSep, string(os.PathSeparator)) {
		binDirWithSep += string(os.PathSeparator)
	}
	if linkAbs != binDirAbs && !strings.HasPrefix(linkAbs, binDirWithSep) {
		return fmt.Errorf("refusing to create link outside bin directory: %s", linkAbs)
	}

	if err := os.Remove(linkAbs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing link %q: %w", linkAbs, err)
	}
	return os.Symlink(target, linkAbs)
}

// UnlinkBin removes a symlink from BinDir.
func (inst *Installer) UnlinkBin(name string) error {
	if !validation.IsValidName(name) {
		return fmt.Errorf("invalid binary name: %q", name)
	}
	return os.Remove(filepath.Join(inst.BinDir, name))
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
		nested := filepath.Join(stageDir, e.Name(), appName)
		if info, err := os.Stat(nested); err == nil && info.IsDir() {
			return nested, nil
		}
		if e.Name() == appName {
			return filepath.Join(stageDir, e.Name()), nil
		}
	}

	return "", fmt.Errorf("could not find %s in extracted archive", appName)
}
