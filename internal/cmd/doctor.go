package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/tap"
)

// doctorCheck is a named diagnostic check.
type doctorCheck struct {
	Name string
	Desc string
	Run  func(ctx *doctorCtx)
}

// doctorCtx carries shared state through all checks.
type doctorCtx struct {
	paths    config.Paths
	cel      *cellar.Cellar
	lnk      *linker.Linker
	loader   *formula.Loader
	formulas []*formula.Formula
	packages []cellar.InstalledPackage
	warnings int
	quiet    bool
}

func (ctx *doctorCtx) warn(format string, args ...any) {
	ctx.warnings++
	fmt.Printf("Warning: "+format+"\n", args...)
}

// allChecks returns the ordered list of doctor checks.
// Security-critical checks come first.
func allChecks() []doctorCheck {
	return []doctorCheck{
		// --- Security checks ---
		{"check_directory_permissions", "Check grew directories are not world-writable", checkDirectoryPermissions},
		{"check_formula_https", "Check all formula URLs use HTTPS", checkFormulaHTTPS},
		{"check_formula_sha256", "Check all formula SHA256 hashes are valid hex", checkFormulaSHA256},
		{"check_symlink_targets", "Check symlinks don't escape the grew prefix", checkSymlinkTargets},
		{"check_cellar_permissions", "Check installed kegs are not world-writable", checkCellarPermissions},
		// --- Structural checks ---
		{"check_directories", "Check required directories exist", checkDirectories},
		{"check_path", "Check grew bin/ is in PATH", checkPath},
		{"check_core_tap", "Check core tap has formulas", checkCoreTap},
		{"check_broken_symlinks", "Check for broken symlinks in bin/, lib/, include/", checkBrokenSymlinks},
		{"check_broken_opt_symlinks", "Check for broken opt/ symlinks", checkBrokenOptSymlinks},
		{"check_unlinked_kegs", "Check installed formulas are linked", checkUnlinkedKegs},
		{"check_orphaned_symlinks", "Check for orphaned symlinks", checkOrphanedSymlinks},
		{"check_multiple_versions", "Check for multiple installed versions", checkMultipleVersions},
		{"check_stale_tmp", "Check for stale files in tmp/", checkStaleTmp},
	}
}

func runDoctor(args []string) error {
	listChecks := false
	auditDebug := false
	quiet := false
	runAll := false
	var selectedChecks []string

	for _, a := range args {
		switch a {
		case "--list-checks":
			listChecks = true
		case "-D", "--audit-debug":
			auditDebug = true
		case "-q", "--quiet":
			quiet = true
		case "--all", "-a":
			runAll = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s\nRun 'grew help doctor' for usage", a)
			}
			selectedChecks = append(selectedChecks, a)
		}
	}

	checks := allChecks()

	if listChecks {
		for _, c := range checks {
			fmt.Printf("%-35s %s\n", c.Name, c.Desc)
		}
		return nil
	}

	// --all overrides any individually selected checks.
	if runAll {
		selectedChecks = nil
	}

	// Filter to selected checks if any specified.
	if len(selectedChecks) > 0 {
		byName := make(map[string]doctorCheck, len(checks))
		for _, c := range checks {
			byName[c.Name] = c
		}
		var filtered []doctorCheck
		for _, name := range selectedChecks {
			c, ok := byName[name]
			if !ok {
				return fmt.Errorf("unknown check: %s\nRun 'grew doctor --list-checks' to see available checks", name)
			}
			filtered = append(filtered, c)
		}
		checks = filtered
	}

	paths := config.Default()

	tapMgr := &tap.Manager{TapsDir: paths.Taps, EmbeddedFS: embeddedTaps}
	if err := tapMgr.InitCore(); err != nil && !quiet {
		fmt.Fprintf(os.Stderr, "Warning: failed to init core tap: %v\n", err)
	}

	loader := newLoader(paths.Taps)
	formulas, _ := loader.LoadAll()

	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}
	packages, _ := cel.List()

	ctx := &doctorCtx{
		paths:    paths,
		cel:      cel,
		lnk:      lnk,
		loader:   loader,
		formulas: formulas,
		packages: packages,
		quiet:    quiet,
	}

	if !quiet {
		fmt.Println("Checking grew installation...")
	}

	for _, c := range checks {
		if auditDebug {
			start := time.Now()
			c.Run(ctx)
			fmt.Printf("[audit] %-35s %s (%d warning(s))\n", c.Name, time.Since(start), ctx.warnings)
		} else {
			c.Run(ctx)
		}
	}

	if ctx.warnings == 0 {
		if !quiet {
			fmt.Println("Your system is ready to brew.")
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\n%d warning(s) found.\n", ctx.warnings)
	}
	// Return error so exit code is non-zero when warnings exist.
	return fmt.Errorf("%d problem(s) detected", ctx.warnings)
}

// --- Security checks ---

func checkDirectoryPermissions(ctx *doctorCtx) {
	dirs := []string{ctx.paths.Root, ctx.paths.Cellar, ctx.paths.Bin, ctx.paths.Opt, ctx.paths.Taps}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		perm := info.Mode().Perm()
		if perm&0002 != 0 {
			ctx.warn("directory %s is world-writable (%o), this is a security risk", dir, perm)
		}
		if perm&0020 != 0 {
			// Group-writable is less severe but still notable for a package manager
			Logf("    Note: %s is group-writable (%o)\n", dir, perm)
		}
	}
}

func checkFormulaHTTPS(ctx *doctorCtx) {
	for _, f := range ctx.formulas {
		for platform, u := range f.URL {
			if !strings.HasPrefix(u, "https://") {
				ctx.warn("formula %s: URL for %s uses insecure HTTP: %s", f.Name, platform, u)
			}
		}
	}
}

func checkFormulaSHA256(ctx *doctorCtx) {
	for _, f := range ctx.formulas {
		for platform, hash := range f.SHA256 {
			if len(hash) != 64 {
				ctx.warn("formula %s: SHA256 for %s has wrong length (%d, expected 64)", f.Name, platform, len(hash))
				continue
			}
			for _, c := range hash {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
					ctx.warn("formula %s: SHA256 for %s contains non-hex character %q", f.Name, platform, string(c))
					break
				}
			}
		}
	}
}

func checkSymlinkTargets(ctx *doctorCtx) {
	absPrefix, err := filepath.Abs(ctx.paths.Root)
	if err != nil {
		return
	}
	for _, dir := range []string{ctx.paths.Bin, ctx.paths.Lib, ctx.paths.Include, ctx.paths.Opt} {
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
			resolved, err := filepath.Abs(target)
			if err != nil {
				continue
			}
			if !strings.HasPrefix(resolved, absPrefix+string(filepath.Separator)) {
				ctx.warn("symlink escapes grew prefix: %s -> %s (resolves to %s)", fullPath, target, resolved)
			}
		}
	}
}

func checkCellarPermissions(ctx *doctorCtx) {
	for _, pkg := range ctx.packages {
		info, err := os.Stat(pkg.Path)
		if err != nil {
			continue
		}
		perm := info.Mode().Perm()
		if perm&0002 != 0 {
			ctx.warn("keg %s/%s is world-writable (%o)", pkg.Name, pkg.Version, perm)
		}
		// Spot-check bin/ inside the keg
		binDir := filepath.Join(pkg.Path, "bin")
		entries, err := os.ReadDir(binDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			binInfo, err := os.Stat(filepath.Join(binDir, e.Name()))
			if err != nil {
				continue
			}
			bp := binInfo.Mode().Perm()
			if bp&0002 != 0 {
				ctx.warn("binary %s/%s/bin/%s is world-writable (%o)", pkg.Name, pkg.Version, e.Name(), bp)
			}
		}
	}
}

// --- Structural checks ---

func checkDirectories(ctx *doctorCtx) {
	required := map[string]string{
		"prefix":  ctx.paths.Root,
		"Cellar":  ctx.paths.Cellar,
		"opt":     ctx.paths.Opt,
		"bin":     ctx.paths.Bin,
		"lib":     ctx.paths.Lib,
		"include": ctx.paths.Include,
		"Taps":    ctx.paths.Taps,
		"CoreTap": ctx.paths.CoreTap,
		"tmp":     ctx.paths.Tmp,
	}
	// Sort for deterministic output.
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dir := required[name]
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			ctx.warn("%s directory missing: %s", name, dir)
		}
	}
}

func checkPath(ctx *doctorCtx) {
	entries := filepath.SplitList(os.Getenv("PATH"))
	for _, entry := range entries {
		abs, err := filepath.Abs(entry)
		if err != nil {
			continue
		}
		binAbs, _ := filepath.Abs(ctx.paths.Bin)
		if abs == binAbs {
			return
		}
	}
	ctx.warn("%s is not in your PATH\n  Add this to your shell profile: eval \"$(grew shellenv)\"", ctx.paths.Bin)
}

func checkCoreTap(ctx *doctorCtx) {
	if len(ctx.formulas) == 0 {
		ctx.warn("no formulas found in any tap")
	}
}

func checkBrokenSymlinks(ctx *doctorCtx) {
	for _, dir := range []string{ctx.paths.Bin, ctx.paths.Lib, ctx.paths.Include} {
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
			if _, err := os.Stat(target); os.IsNotExist(err) {
				ctx.warn("broken symlink: %s -> %s", fullPath, target)
			}
		}
	}
}

func checkBrokenOptSymlinks(ctx *doctorCtx) {
	entries, err := os.ReadDir(ctx.paths.Opt)
	if err != nil {
		return
	}
	for _, e := range entries {
		fullPath := filepath.Join(ctx.paths.Opt, e.Name())
		target, err := os.Readlink(fullPath)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(ctx.paths.Opt, target)
		}
		if _, err := os.Stat(target); os.IsNotExist(err) {
			ctx.warn("broken opt symlink: %s -> %s", fullPath, target)
		}
	}
}

func checkUnlinkedKegs(ctx *doctorCtx) {
	for _, pkg := range ctx.packages {
		if ctx.lnk.IsLinked(pkg.Name) {
			continue
		}
		f, err := ctx.loader.LoadByName(pkg.Name)
		if err == nil && f.KegOnly {
			continue
		}
		ctx.warn("%s %s is installed but not linked", pkg.Name, pkg.Version)
	}
}

func checkOrphanedSymlinks(ctx *doctorCtx) {
	for _, dir := range []string{ctx.paths.Bin, ctx.paths.Lib, ctx.paths.Include} {
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
			target = filepath.Clean(target)
			if !strings.Contains(target, "Cellar") {
				continue
			}
			rel, err := filepath.Rel(ctx.paths.Cellar, target)
			if err != nil {
				continue
			}
			name := strings.SplitN(rel, string(filepath.Separator), 2)[0]
			if !ctx.cel.IsInstalled(name) {
				ctx.warn("orphaned symlink: %s (formula %q not installed)", fullPath, name)
			}
		}
	}
}

func checkMultipleVersions(ctx *doctorCtx) {
	for _, pkg := range ctx.packages {
		versions, err := ctx.cel.InstalledVersions(pkg.Name)
		if err != nil || len(versions) <= 1 {
			continue
		}
		ctx.warn("%s has %d versions installed (%s), consider running 'grew cleanup'",
			pkg.Name, len(versions), strings.Join(versions, ", "))
	}
}

func checkStaleTmp(ctx *doctorCtx) {
	entries, err := os.ReadDir(ctx.paths.Tmp)
	if err == nil && len(entries) > 0 {
		ctx.warn("%d leftover file(s) in tmp directory, consider running 'grew cleanup'", len(entries))
	}
}
