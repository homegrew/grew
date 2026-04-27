package doctor

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/snapshot"
	grewrt "github.com/homegrew/grew/internal/runtime"
	"github.com/homegrew/grew/pkg/safepath"
)

// Check is a named diagnostic check.
type Check struct {
	Name string
	Desc string
	Run  func(ctx *Context)
}

// Context carries shared state through all checks.
type Context struct {
	Paths    config.Paths
	Cel      *cellar.Cellar
	Lnk      *linker.Linker
	Loader   *formula.Loader
	Formulas []*formula.Formula
	Casks    []*cask.Cask
	Packages []cellar.InstalledPackage
	Warnings int
	Quiet    bool
	Warn     func(format string, args ...any)
}

// ExtraChecks holds platform-specific checks registered via init().
var ExtraChecks []Check

// RegisterExtraChecks appends checks from platform-specific files.
func RegisterExtraChecks(checks []Check) {
	ExtraChecks = append(ExtraChecks, checks...)
}

// BaseChecks returns the ordered list of base doctor checks.
func BaseChecks() []Check {
	return []Check{
		// --- Security checks ---
		{"check_prefix_isolation", "Check grew prefix is outside $HOME", CheckPrefixIsolation},
		{"check_directory_permissions", "Check grew directories are not world-writable", CheckDirectoryPermissions},
		{"check_formula_https", "Check all formula URLs use HTTPS", CheckFormulaHTTPS},
		{"check_formula_sha256", "Check all formula SHA256 hashes are valid hex", CheckFormulaSHA256},
		//{"check_formula_sha512", "Check all formula SHA512 hashes are valid hex", CheckFormulaSHA512},
		{"check_cask_sha256", "Check all cask SHA256 hashes are valid hex", CheckCaskSHA256},
		//{"check_cask_sha512", "Check all cask SHA512 hashes are valid hex", CheckCaskSHA512},
		{"check_symlink_targets", "Check symlinks don't escape the grew prefix", CheckSymlinkTargets},
		{"check_cellar_permissions", "Check installed kegs are not world-writable", CheckCellarPermissions},
		{"check_incomplete_installs", "Check for packages missing an installation manifest", CheckIncompleteInstalls},
		{"check_snapshot_integrity", "Verify installed packages against their manifests", CheckSnapshotIntegrity},
		{"check_sandbox", "Verify functional sandboxing is available", CheckSandbox},
		// --- Structural / health checks ---
		{"check_directories", "Check required directories exist", CheckDirectories},
		{"check_path", "Check grew bin/ is in PATH", CheckPath},
		{"check_core_tap", "Check core tap has formulas", CheckCoreTap},
		{"check_broken_symlinks", "Check for broken symlinks in bin/, lib/, include/", CheckBrokenSymlinks},
		{"check_broken_opt_symlinks", "Check for broken opt/ symlinks", CheckBrokenOptSymlinks},
		{"check_unlinked_kegs", "Check installed formulas are linked", CheckUnlinkedKegs},
		{"check_orphaned_symlinks", "Check for orphaned symlinks", CheckOrphanedSymlinks},
		{"check_multiple_versions", "Check for multiple installed versions", CheckMultipleVersions},
		{"check_pinned_formulas", "Check for pinned formulas", CheckPinnedFormulas},
		{"check_stale_tmp", "Check for stale files in tmp/", CheckStaleTmp},
	}
}

// SymlinkInfo holds a resolved symlink found in a directory.
type SymlinkInfo struct {
	Path   string // full path of the symlink
	Target string // resolved absolute target
}

// WalkSymlinks iterates symlinks in the given directories, calling fn for each.
func WalkSymlinks(dirs []string, fn func(info SymlinkInfo)) {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			fullPath := filepath.Join(dir, e.Name())
			target, err := os.Readlink(fullPath)
			if err != nil {
				continue
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			fn(SymlinkInfo{Path: fullPath, Target: target})
		}
	}
}

func ValidHexHash(hash string, expectedLen int) string {
	hash = strings.TrimSpace(hash)
	if hash == "" || hash == "no_check" {
		return ""
	}
	if len(hash) != expectedLen {
		return fmt.Sprintf("has wrong length (%d, expected %d)", len(hash), expectedLen)
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return fmt.Sprintf("contains non-hex character %q", string(c))
		}
	}
	return ""
}

// --- Security checks ---

func CheckPrefixIsolation(ctx *Context) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if strings.HasPrefix(ctx.Paths.Root, home+string(filepath.Separator)) {
		ctx.Warn("grew prefix %s is under $HOME — sandboxed builds can potentially access "+
			"sensitive files (e.g. ~/.ssh, ~/.gnupg).\n"+
			"  Run 'sudo grew setup' to install to %s for better isolation.",
			ctx.Paths.Root, grewrt.SystemPrefix())
	}
}

func CheckDirectoryPermissions(ctx *Context) {
	dirs := []string{ctx.Paths.Root, ctx.Paths.Cellar, ctx.Paths.Bin, ctx.Paths.Opt, ctx.Paths.Taps}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		perm := info.Mode().Perm()
		if perm&0002 != 0 {
			ctx.Warn("directory %s is world-writable (%o), this is a security risk", dir, perm)
		}
		if perm&0020 != 0 {
			slog.Info(fmt.Sprintf("note: %s is group-writable (%o)", dir, perm))
		}
	}
}

func CheckFormulaHTTPS(ctx *Context) {
	for _, f := range ctx.Formulas {
		for platform, u := range f.URL {
			if !strings.HasPrefix(u, "https://") {
				ctx.Warn("formula %s: URL for %s uses insecure HTTP: %s", f.Name, platform, u)
			}
		}
	}
}

func CheckFormulaSHA256(ctx *Context) {
	for _, f := range ctx.Formulas {
		for platform, hash := range f.SHA256 {
			if msg := ValidHexHash(hash, 64); msg != "" {
				ctx.Warn("formula %s: SHA256 for %s %s", f.Name, platform, msg)
			}
		}
	}
}

func CheckFormulaSHA512(ctx *Context) {
	for _, f := range ctx.Formulas {
		if len(f.SHA512) == 0 && len(f.Bottle) == 0 {
			// Only warn if SHA256 is present but SHA512 is not.
			if len(f.SHA256) > 0 {
				ctx.Warn("formula %s: missing SHA512 metadata", f.Name)
			}
			continue
		}

		for platform, hash := range f.SHA512 {
			if msg := ValidHexHash(hash, 128); msg != "" {
				ctx.Warn("formula %s: SHA512 for %s %s", f.Name, platform, msg)
			}
		}

		for platform, b := range f.Bottle {
			if b.SHA512 == "" {
				ctx.Warn("formula %s: bottle for %s missing SHA512", f.Name, platform)
				continue
			}
			if msg := ValidHexHash(b.SHA512, 128); msg != "" {
				ctx.Warn("formula %s: bottle SHA512 for %s %s", f.Name, platform, msg)
			}
		}
	}
}

func CheckCaskSHA256(ctx *Context) {
	for _, c := range ctx.Casks {
		for platform, hash := range c.SHA256 {
			if msg := ValidHexHash(hash, 64); msg != "" {
				ctx.Warn("cask %s: SHA256 for %s %s", c.Name, platform, msg)
			}
		}
	}
}

func CheckCaskSHA512(ctx *Context) {
	for _, c := range ctx.Casks {
		if len(c.SHA512) == 0 {
			// Only warn if SHA256 is present but SHA512 is not.
			if len(c.SHA256) > 0 {
				ctx.Warn("cask %s: missing SHA512 metadata", c.Name)
			}
			continue
		}

		for platform, hash := range c.SHA512 {
			if msg := ValidHexHash(hash, 128); msg != "" {
				ctx.Warn("cask %s: SHA512 for %s %s", c.Name, platform, msg)
			}
		}
	}
}

func CheckSymlinkTargets(ctx *Context) {
	absPrefix, err := filepath.Abs(ctx.Paths.Root)
	if err != nil {
		return
	}
	WalkSymlinks([]string{ctx.Paths.Bin, ctx.Paths.Lib, ctx.Paths.Include, ctx.Paths.Opt}, func(si SymlinkInfo) {
		resolved, err := filepath.Abs(si.Target)
		if err != nil {
			return
		}
		if !safepath.IsSubpath(absPrefix, resolved) {
			ctx.Warn("symlink escapes grew prefix: %s -> %s (resolves to %s)", si.Path, si.Target, resolved)
		}
	})
}

func CheckCellarPermissions(ctx *Context) {
	for _, pkg := range ctx.Packages {
		info, err := os.Stat(pkg.Path)
		if err != nil {
			continue
		}
		perm := info.Mode().Perm()
		if perm&0002 != 0 {
			ctx.Warn("keg %s/%s is world-writable (%o)", pkg.Name, pkg.Version, perm)
		}
		binDir := filepath.Join(pkg.Path, "bin")
		entries, err := os.ReadDir(binDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			entryPath, safeJoinErr := safepath.SafeJoin(binDir, e.Name())
			if safeJoinErr != nil {
				continue
			}
			binInfo, statErr := os.Stat(entryPath)
			if statErr != nil {
				continue
			}
			bp := binInfo.Mode().Perm()
			if bp&0002 != 0 {
				ctx.Warn("binary %s/%s/bin/%s is world-writable (%o)", pkg.Name, pkg.Version, e.Name(), bp)
			}
		}
	}
}

func CheckIncompleteInstalls(ctx *Context) {
	for _, pkg := range ctx.Packages {
		kegPath, err := ctx.Cel.KegPath(pkg.Name, pkg.Version)
		if err != nil {
			continue
		}
		if !snapshot.Exists(kegPath) {
			ctx.Warn("%s %s: installation manifest missing — this package may be half-installed.\n"+
				"  Run 'grew reinstall %s' to fix.", pkg.Name, pkg.Version, pkg.Name)
		}
	}
}

func CheckSnapshotIntegrity(ctx *Context) {
	for _, pkg := range ctx.Packages {
		kegPath, err := ctx.Cel.KegPath(pkg.Name, pkg.Version)
		if err != nil {
			continue
		}
		if !snapshot.Exists(kegPath) {
			continue
		}
		result, err := snapshot.Verify(kegPath)
		if err != nil {
			ctx.Warn("%s %s: snapshot verification error: %v", pkg.Name, pkg.Version, err)
			continue
		}
		if result.OK {
			continue
		}
		for _, f := range result.Missing {
			ctx.Warn("%s %s: missing file: %s", pkg.Name, pkg.Version, f)
		}
		for _, f := range result.Modified {
			ctx.Warn("%s %s: modified: %s", pkg.Name, pkg.Version, f)
		}
		for _, f := range result.Added {
			ctx.Warn("%s %s: unexpected file: %s", pkg.Name, pkg.Version, f)
		}
		if result.KegSHA256Mismatch {
			ctx.Warn("%s %s: aggregate SHA256 mismatch", pkg.Name, pkg.Version)
		}
		if result.KegSHA512Mismatch {
			ctx.Warn("%s %s: aggregate SHA512 mismatch", pkg.Name, pkg.Version)
		}
		for _, e := range result.Errors {
			ctx.Warn("%s %s: %s", pkg.Name, pkg.Version, e)
		}
	}
}

func CheckSandbox(ctx *Context) {
	if !sandbox.IsSandboxed() {
		ctx.Warn("Functional sandboxing is NOT available on this system.\n" +
			"  Source builds and post-install scripts will run without isolation.")
	}
}

// --- Structural checks ---

func CheckDirectories(ctx *Context) {
	required := map[string]string{
		"prefix":  ctx.Paths.Root,
		"Cellar":  ctx.Paths.Cellar,
		"opt":     ctx.Paths.Opt,
		"bin":     ctx.Paths.Bin,
		"lib":     ctx.Paths.Lib,
		"include": ctx.Paths.Include,
		"Taps":    ctx.Paths.Taps,
		"CoreTap": ctx.Paths.CoreTap,
		"tmp":     ctx.Paths.Tmp,
	}
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dir := required[name]
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			ctx.Warn("%s directory missing: %s", name, dir)
		}
	}
}

func CheckPath(ctx *Context) {
	entries := filepath.SplitList(os.Getenv("PATH"))
	for _, entry := range entries {
		abs, err := filepath.Abs(entry)
		if err != nil {
			continue
		}
		binAbs, _ := filepath.Abs(ctx.Paths.Bin)
		if abs == binAbs {
			return
		}
	}
	ctx.Warn("%s is not in your PATH\n  Add this to your shell profile: eval \"$(grew shellenv)\"", ctx.Paths.Bin)
}

func CheckCoreTap(ctx *Context) {
	if len(ctx.Formulas) == 0 {
		ctx.Warn("no formulas found in any tap")
	}
}

func CheckBrokenSymlinks(ctx *Context) {
	WalkSymlinks([]string{ctx.Paths.Bin, ctx.Paths.Lib, ctx.Paths.Include}, func(si SymlinkInfo) {
		if _, err := os.Stat(si.Target); os.IsNotExist(err) {
			ctx.Warn("broken symlink: %s -> %s", si.Path, si.Target)
		}
	})
}

func CheckBrokenOptSymlinks(ctx *Context) {
	WalkSymlinks([]string{ctx.Paths.Opt}, func(si SymlinkInfo) {
		if _, err := os.Stat(si.Target); os.IsNotExist(err) {
			ctx.Warn("broken opt symlink: %s -> %s", si.Path, si.Target)
		}
	})
}

func CheckUnlinkedKegs(ctx *Context) {
	for _, pkg := range ctx.Packages {
		if ctx.Lnk.IsLinked(pkg.Name) {
			continue
		}
		f, err := ctx.Loader.LoadByName(pkg.Name)
		if err == nil && f.KegOnly {
			continue
		}
		ctx.Warn("%s %s is installed but not linked", pkg.Name, pkg.Version)
	}
}

func CheckOrphanedSymlinks(ctx *Context) {
	WalkSymlinks([]string{ctx.Paths.Bin, ctx.Paths.Lib, ctx.Paths.Include}, func(si SymlinkInfo) {
		target := filepath.Clean(si.Target)
		if !strings.Contains(target, "Cellar") {
			return
		}
		rel, err := filepath.Rel(ctx.Paths.Cellar, target)
		if err != nil {
			return
		}
		name := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if !ctx.Cel.IsInstalled(name) {
			ctx.Warn("orphaned symlink: %s (formula %q not installed)", si.Path, name)
		}
	})
}

func CheckMultipleVersions(ctx *Context) {
	for _, pkg := range ctx.Packages {
		versions, err := ctx.Cel.InstalledVersions(pkg.Name)
		if err != nil || len(versions) <= 1 {
			continue
		}
		ctx.Warn("%s has %d versions installed (%s), consider running 'grew cleanup'",
			pkg.Name, len(versions), strings.Join(versions, ", "))
	}
}

func CheckPinnedFormulas(ctx *Context) {
	var pinned []string
	for _, pkg := range ctx.Packages {
		if ctx.Cel.IsPinned(pkg.Name) {
			pinned = append(pinned, pkg.Name)
		}
	}
	if len(pinned) > 0 {
		ctx.Warn("Some formulas are pinned and will not be upgraded: %s", strings.Join(pinned, ", "))
	}
}

func CheckStaleTmp(ctx *Context) {
	entries, err := os.ReadDir(ctx.Paths.Tmp)
	if err == nil && len(entries) > 0 {
		ctx.Warn("%d leftover file(s) in tmp directory, consider running 'grew cleanup'", len(entries))
	}
}
