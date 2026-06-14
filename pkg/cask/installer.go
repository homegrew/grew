package cask

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/pkg/fsutil"
	"github.com/homegrew/grew/pkg/quarantine"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/sandbox"
	"github.com/homegrew/grew/pkg/sudo"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/homegrew/grew/pkg/validation"
)

// Installer handles placing cask artifacts into their destinations.
type Installer struct {
	AppDir  string // ~/Applications
	BinDir  string // ~/.homegrew/bin
	FontDir string // ~/Library/Fonts
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
	if err := safepath.SafePathComponent(appName); err != nil {
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
	realStage, err := filepath.EvalSymlinks(stageDir)
	if err != nil {
		return "", fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}
	rel, err := filepath.Rel(realStage, realSrc)
	if err != nil {
		return "", fmt.Errorf("resolve relative path from staging directory: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return "", fmt.Errorf("app %s resolves to staging directory itself: %s", appName, realSrc)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("app %s resolves outside staging directory: %s", appName, realSrc)
	}

	destApp, err := safepath.SafeJoin(inst.AppDir, appName)
	if err != nil {
		return "", fmt.Errorf("invalid app destination: %w", err)
	}

	// Remove existing app if present (reinstall)
	if _, err := os.Stat(destApp); err == nil {
		if _, err := quarantine.Trash(destApp); err != nil {
			slog.Warn("failed to move existing app to Trash, falling back to permanent deletion", "app", appName, "error", err)
			if err := os.RemoveAll(destApp); err != nil {
				return "", fmt.Errorf("remove existing %s: %w", appName, err)
			}
		}
	}

	if err := fsutil.CopyTree(srcApp, destApp); err != nil {
		return "", fmt.Errorf("copy %s to %s: %w", appName, inst.AppDir, err)
	}

	return destApp, nil
}

// InstallPkg runs a macOS .pkg installer using sudo installer.
func (inst *Installer) InstallPkg(stageDir, pkgName string) error {
	if !strings.HasSuffix(pkgName, ".pkg") {
		return fmt.Errorf("artifact %q is not a .pkg installer", pkgName)
	}
	if filepath.Base(pkgName) != pkgName {
		return fmt.Errorf("invalid pkg name: %q", pkgName)
	}
	if err := safepath.SafePathComponent(pkgName); err != nil {
		return fmt.Errorf("invalid pkg name %q: %w", pkgName, err)
	}

	srcPkg, err := findPkg(stageDir, pkgName)
	if err != nil {
		return err
	}

	// Containment check: verify srcPkg is within stageDir (symlink escape protection).
	realSrc, err := filepath.EvalSymlinks(srcPkg)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", pkgName, err)
	}
	realStage, err := filepath.EvalSymlinks(stageDir)
	if err != nil {
		return fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}
	rel, err := filepath.Rel(realStage, realSrc)
	if err != nil {
		return fmt.Errorf("resolve relative path from staging directory: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return fmt.Errorf("pkg %s resolves to staging directory itself: %s", pkgName, realSrc)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("pkg %s resolves outside staging directory: %s", pkgName, realSrc)
	}

	ui.FprintArrow(os.Stderr, "Running installer for %s (may prompt for password)", pkgName)
	return sudo.RunSudoCmd("/usr/sbin/installer", "-pkg", realSrc, "-target", "/")
}

// UninstallPkg returns an error as macOS packages are hard to uninstall cleanly.
func (inst *Installer) UninstallPkg(pkgName string) error {
	return fmt.Errorf("automatic uninstallation of .pkg artifacts is not supported: %s", pkgName)
}

// InstallFont copies a font file from the staging directory into FontDir.
// fontRel is the font's path relative to the archive root (it may contain
// subdirectories); only its base name is used as the installed file name.
// It returns the destination path.
func (inst *Installer) InstallFont(stageDir, fontRel string) (string, error) {
	fontName := filepath.Base(fontRel)
	if err := safepath.SafePathComponent(fontName); err != nil {
		return "", fmt.Errorf("invalid font name %q: %w", fontName, err)
	}
	if !fontExts[strings.ToLower(filepath.Ext(fontName))] {
		return "", fmt.Errorf("artifact %q is not a recognized font file", fontRel)
	}
	if err := safepath.SafeAbsolutePath(inst.FontDir); err != nil {
		return "", fmt.Errorf("invalid font directory %q: %w", inst.FontDir, err)
	}

	srcFont, err := findFont(stageDir, fontRel)
	if err != nil {
		return "", err
	}

	// Containment check: verify srcFont resolves within stageDir (symlink escape protection).
	realSrc, err := filepath.EvalSymlinks(srcFont)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", fontName, err)
	}
	realStage, err := filepath.EvalSymlinks(stageDir)
	if err != nil {
		return "", fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}
	rel, err := filepath.Rel(realStage, realSrc)
	if err != nil {
		return "", fmt.Errorf("resolve relative path from staging directory: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("font %s resolves outside staging directory: %s", fontName, realSrc)
	}

	destFont, err := safepath.SafeJoin(inst.FontDir, fontName)
	if err != nil {
		return "", fmt.Errorf("invalid font destination: %w", err)
	}

	if err := os.MkdirAll(inst.FontDir, 0o755); err != nil {
		return "", fmt.Errorf("create font directory: %w", err)
	}
	if err := fsutil.CopyFileWithinRoot(realSrc, destFont, inst.FontDir, 0o644); err != nil {
		return "", fmt.Errorf("copy %s to %s: %w", fontName, inst.FontDir, err)
	}

	return destFont, nil
}

// InstallInstallerScript runs a cask "installer script" artifact: an executable
// bundled in the downloaded archive. It is run under grew's Seatbelt sandbox
// with network access denied and writes confined to the staging directory and
// the grew prefix. $HOMEGREW_PREFIX in the arguments is expanded to prefix.
//
// Scripts that request sudo are refused — grew never runs downloaded code as
// root. This is the deliberate divergence from Homebrew, which would escalate.
func (inst *Installer) InstallInstallerScript(stageDir string, script InstallerScript, prefix string) error {
	if script.Sudo {
		return fmt.Errorf("installer script %q requires sudo, which grew does not allow for downloaded scripts", script.Executable)
	}
	if script.Executable == "" {
		return fmt.Errorf("installer script has no executable")
	}
	if err := safepath.SafePathComponent(filepath.Base(script.Executable)); err != nil {
		return fmt.Errorf("invalid installer executable %q: %w", script.Executable, err)
	}
	if err := safepath.SafeAbsolutePath(prefix); err != nil {
		return fmt.Errorf("invalid prefix %q: %w", prefix, err)
	}

	srcExe, err := findStagedFile(stageDir, script.Executable)
	if err != nil {
		return err
	}

	// Containment check: verify srcExe resolves within stageDir (symlink escape protection).
	realSrc, err := filepath.EvalSymlinks(srcExe)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", script.Executable, err)
	}
	realStage, err := filepath.EvalSymlinks(stageDir)
	if err != nil {
		return fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}
	rel, err := filepath.Rel(realStage, realSrc)
	if err != nil {
		return fmt.Errorf("resolve relative path from staging directory: %w", err)
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("installer script %s resolves outside staging directory: %s", script.Executable, realSrc)
	}

	if err := os.Chmod(realSrc, 0o755); err != nil {
		return fmt.Errorf("make installer script executable: %w", err)
	}

	args := expandPrefixVars(script.Args, prefix)
	sbCfg := sandbox.BuildConfig{BuildDir: realStage, KegDir: prefix}
	cmd := sandbox.Command(sbCfg, realSrc, args...)
	cmd.Dir = realStage
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	ui.FprintArrow(os.Stderr, "Running installer script %s (sandboxed, no sudo)", filepath.Base(script.Executable))
	return cmd.Run()
}

// expandPrefixVars substitutes the grew prefix for the $HOMEGREW_PREFIX (and
// ${HOMEGREW_PREFIX}) placeholders in installer script arguments. The arguments
// are passed directly to the executable, not through a shell, so grew performs
// the expansion itself.
func expandPrefixVars(args []string, prefix string) []string {
	repl := strings.NewReplacer("${HOMEGREW_PREFIX}", prefix, "$HOMEGREW_PREFIX", prefix)
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = repl.Replace(a)
	}
	return out
}

// UninstallFont removes an installed font file from FontDir.
func (inst *Installer) UninstallFont(fontRel string) error {
	fontName := filepath.Base(fontRel)
	if err := safepath.SafePathComponent(fontName); err != nil {
		return fmt.Errorf("invalid font name %q: %w", fontName, err)
	}
	if err := safepath.SafeAbsolutePath(inst.FontDir); err != nil {
		return fmt.Errorf("invalid font directory %q: %w", inst.FontDir, err)
	}
	destFont, err := safepath.SafeJoin(inst.FontDir, fontName)
	if err != nil {
		return fmt.Errorf("invalid font destination: %w", err)
	}
	if info, err := os.Lstat(destFont); err != nil {
		if os.IsNotExist(err) {
			return nil // already gone
		}
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to remove non-regular file at %q", destFont)
	}
	// SafeJoin's containment check is lexical. Resolve symlinks in the font
	// directory and re-verify containment before removing so a symlinked
	// FontDir cannot redirect the removal outside the intended tree.
	resolvedFontDir, err := filepath.EvalSymlinks(inst.FontDir)
	if err != nil {
		return fmt.Errorf("resolve font directory: %w", err)
	}
	resolvedFontDir = filepath.Clean(resolvedFontDir)
	resolvedDest := filepath.Clean(filepath.Join(resolvedFontDir, fontName))
	if err := safepath.CheckSubpath(resolvedFontDir, resolvedDest); err != nil {
		return fmt.Errorf("refusing to remove file outside font directory: %w", err)
	}
	return os.Remove(destFont)
}

// UninstallApp removes a .app bundle from AppDir by moving it to the Trash.
func (inst *Installer) UninstallApp(appName string) error {
	if filepath.Base(appName) != appName {
		return fmt.Errorf("invalid app name: %q", appName)
	}
	if err := safepath.SafePathComponent(appName); err != nil {
		return fmt.Errorf("invalid app name %q: %w", appName, err)
	}
	if err := safepath.SafeAbsolutePath(inst.AppDir); err != nil {
		return fmt.Errorf("invalid app base directory %q: %w", inst.AppDir, err)
	}

	destApp, err := safepath.SafeJoin(inst.AppDir, appName)
	if err != nil {
		return fmt.Errorf("invalid app destination: %w", err)
	}

	realBase, baseErr := filepath.EvalSymlinks(inst.AppDir)
	if baseErr != nil {
		realBase, baseErr = filepath.Abs(filepath.Clean(inst.AppDir))
		if baseErr != nil {
			return fmt.Errorf("resolve app base directory %q: %w", inst.AppDir, baseErr)
		}
	}
	realDest, destErr := filepath.EvalSymlinks(destApp)
	if destErr != nil {
		if errors.Is(destErr, os.ErrNotExist) {
			realDest = filepath.Clean(filepath.Join(realBase, appName))
			destErr = nil
		}
		if destErr != nil {
			return fmt.Errorf("resolve app path %q: %w", destApp, destErr)
		}
	}
	if err := safepath.CheckSubpath(realBase, realDest); err != nil {
		return fmt.Errorf("invalid app destination containment: %w", err)
	}

	if _, err := os.Stat(destApp); os.IsNotExist(err) {
		return nil // already gone
	}

	if _, err := quarantine.Trash(destApp); err != nil {
		slog.Warn("failed to move app to Trash, falling back to permanent deletion", "app", appName, "error", err)
		return os.RemoveAll(destApp)
	}
	return nil
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

	linkAbs, err := safepath.SafeJoin(binDirAbs, name)
	if err != nil {
		return fmt.Errorf("refusing to create link outside bin directory: %w", err)
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

	// Ensure we only operate within the configured BinDir.
	binAbs, err := filepath.Abs(inst.BinDir)
	if err != nil {
		return fmt.Errorf("resolve bin dir: %w", err)
	}
	binAbs = filepath.Clean(binAbs)

	linkAbs, err := safepath.SafeJoin(binAbs, name)
	if err != nil {
		return fmt.Errorf("refusing to remove path outside bin directory: %w", err)
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
	if err := safepath.SafePathComponent(appName); err != nil {
		return "", fmt.Errorf("invalid app name %q: %w", appName, err)
	}

	stageAbs, err := filepath.Abs(stageDir)
	if err != nil {
		return "", fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}

	// First, look for a top-level bundle: <stageDir>/<appName>.
	direct, err := safepath.SafeJoin(stageAbs, appName)
	if err == nil {
		if info, err := os.Stat(direct); err == nil && info.IsDir() {
			return direct, nil
		}
	}

	// If not found, walk one level deep and look for <stageDir>/*/<appName>.
	entries, err := os.ReadDir(stageAbs)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(e.Name()); err != nil {
			continue
		}
		nested, err := safepath.SafeJoin(stageAbs, e.Name(), appName)
		if err != nil {
			continue
		}
		if info, err := os.Stat(nested); err == nil && info.IsDir() {
			return nested, nil
		}
	}

	return "", fmt.Errorf("could not find %s in extracted archive", appName)
}

// findPkg searches stageDir for a .pkg installer with the given name.
func findPkg(stageDir, pkgName string) (string, error) {
	if err := safepath.SafePathComponent(pkgName); err != nil {
		return "", fmt.Errorf("invalid pkg name %q: %w", pkgName, err)
	}

	stageAbs, err := filepath.Abs(stageDir)
	if err != nil {
		return "", fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}

	// First, look for a top-level bundle: <stageDir>/<pkgName>.
	direct, err := safepath.SafeJoin(stageAbs, pkgName)
	if err == nil {
		if info, err := os.Stat(direct); err == nil && !info.IsDir() {
			return direct, nil
		}
		// Some .pkg files are directories (bundles)
		if info, err := os.Stat(direct); err == nil && info.IsDir() {
			return direct, nil
		}
	}

	// If not found, walk one level deep and look for <stageDir>/*/<pkgName>.
	entries, err := os.ReadDir(stageAbs)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := safepath.SafePathComponent(e.Name()); err != nil {
			continue
		}
		nested, err := safepath.SafeJoin(stageAbs, e.Name(), pkgName)
		if err != nil {
			continue
		}
		if _, err := os.Stat(nested); err == nil {
			return nested, nil
		}
	}

	return "", fmt.Errorf("could not find %s in extracted archive", pkgName)
}

// findFont locates a font file inside stageDir by its archive-relative path.
func findFont(stageDir, fontRel string) (string, error) {
	return findStagedFile(stageDir, fontRel)
}

// findStagedFile locates a regular file inside stageDir. It first tries the
// exact path rel relative to the archive root, then falls back to searching the
// tree for any regular file whose base name matches. The returned path is
// always contained within stageDir.
func findStagedFile(stageDir, rel string) (string, error) {
	stageAbs, err := filepath.Abs(stageDir)
	if err != nil {
		return "", fmt.Errorf("resolve staging directory %s: %w", stageDir, err)
	}

	// Try the exact relative path first. SafeJoin validates each component and
	// guarantees the result stays within stageAbs.
	components := strings.FieldsFunc(filepath.ToSlash(rel), func(r rune) bool { return r == '/' })
	if len(components) > 0 {
		if direct, err := safepath.SafeJoin(stageAbs, components...); err == nil {
			// Resolve symlinks and re-check containment so a symlink inside the
			// archive cannot redirect to a path outside stageDir.
			if real, err := filepath.EvalSymlinks(direct); err == nil {
				if err := safepath.CheckSubpath(stageAbs, real); err == nil {
					if info, err := os.Lstat(real); err == nil && info.Mode().IsRegular() {
						return real, nil
					}
				}
			}
		}
	}

	// Fall back to a tree search by base name.
	base := filepath.Base(rel)
	var found string
	_ = filepath.WalkDir(stageAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() && d.Type().IsRegular() && filepath.Base(path) == base {
			if safepath.IsSubpath(stageAbs, path) {
				found = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if found != "" {
		return found, nil
	}

	return "", fmt.Errorf("could not find %s in extracted archive", base)
}
