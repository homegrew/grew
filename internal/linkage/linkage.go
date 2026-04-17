// Package linkage inspects dynamic library dependencies of installed kegs
// and classifies them as system, self, other-formula, or broken.
package linkage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/homegrew/grew/pkg/validation"
)

// DepKind classifies a dynamic library dependency.
type DepKind int

const (
	System   DepKind = iota // OS-provided (/usr/lib, /System/Library, etc.)
	Self                    // provided by the keg itself
	OtherKeg                // provided by another formula in the Cellar
	Variable                // uses @rpath, @loader_path, @executable_path, or $ORIGIN
	Broken                  // cannot be resolved on disk
)

func (k DepKind) String() string {
	switch k {
	case System:
		return "system"
	case Self:
		return "self"
	case OtherKeg:
		return "other"
	case Variable:
		return "variable"
	case Broken:
		return "broken"
	default:
		return "unknown"
	}
}

// Dep describes a single dynamic library reference found in a binary.
type Dep struct {
	Path     string  `json:"path"`
	Kind     DepKind `json:"kind"`
	Resolved string  `json:"resolved,omitempty"`
	Formula  string  `json:"formula,omitempty"`
}

// BinaryResult holds the linkage analysis for one binary file.
type BinaryResult struct {
	Path string `json:"path"`
	Deps []Dep  `json:"deps"`
}

// Result holds the full linkage analysis for a keg.
type Result struct {
	Name     string         `json:"name"`
	Version  string         `json:"version"`
	KegPath  string         `json:"keg_path"`
	Binaries []BinaryResult `json:"binaries"`
}

// Broken returns all broken dependencies across all binaries.
func (r *Result) Broken() []Dep {
	var out []Dep
	for _, b := range r.Binaries {
		for _, d := range b.Deps {
			if d.Kind == Broken {
				out = append(out, d)
			}
		}
	}
	return out
}

// LinkedFormulas returns the deduplicated set of other-formula names that
// binaries in this keg link against.
func (r *Result) LinkedFormulas() []string {
	seen := make(map[string]bool)
	var out []string
	for _, b := range r.Binaries {
		for _, d := range b.Deps {
			if d.Formula != "" && !seen[d.Formula] {
				seen[d.Formula] = true
				out = append(out, d.Formula)
			}
		}
	}
	return out
}

// StrictResult holds additional diagnostics produced by strict-mode checking.
type StrictResult struct {
	Undeclared []string // formulas linked against but not in declared deps
	Unused     []string // declared deps not linked by any binary
}

// Strict compares the actual linked formulas against the declared dependency
// list and reports undeclared and unused dependencies.
func (r *Result) Strict(declaredDeps []string) StrictResult {
	linked := make(map[string]bool)
	for _, f := range r.LinkedFormulas() {
		linked[f] = true
	}

	declared := make(map[string]bool)
	for _, d := range declaredDeps {
		declared[d] = true
	}

	var sr StrictResult
	for f := range linked {
		if !declared[f] {
			sr.Undeclared = append(sr.Undeclared, f)
		}
	}
	for _, d := range declaredDeps {
		if !linked[d] {
			sr.Unused = append(sr.Unused, d)
		}
	}
	return sr
}

// Check inspects all binaries in the keg at kegPath and classifies their
// dynamic library dependencies.
func Check(name, version, kegPath, cellarPath string) (*Result, error) {
	res := &Result{
		Name:    name,
		Version: version,
		KegPath: kegPath,
	}

	err := filepath.WalkDir(kegPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if rel, relErr := filepath.Rel(kegPath, path); relErr != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		if !isBinary(path) {
			return nil
		}

		deps, rpaths, inspectErr := inspectDeps(path)
		if inspectErr != nil {
			return nil // skip uninspectable binaries
		}
		if len(deps) == 0 {
			return nil
		}

		br := BinaryResult{Path: path}
		for _, dep := range deps {
			br.Deps = append(br.Deps, classifyDep(dep, path, rpaths, kegPath, cellarPath))
		}
		res.Binaries = append(res.Binaries, br)
		return nil
	})

	return res, err
}

// classifyDep determines the kind of a single dependency path.
func classifyDep(depPath, binaryPath string, rpaths []string, kegPath, cellarPath string) Dep {
	dep := Dep{Path: depPath}

	// Handle variable references (@rpath, @loader_path, @executable_path, $ORIGIN).
	if isVariableRef(depPath) {
		resolved := resolveVariable(depPath, binaryPath, rpaths)
		if resolved != "" {
			dep.Kind = Variable
			dep.Resolved = resolved
			// Further classify the resolved target.
			if inner := classifyAbsPath(resolved, kegPath, cellarPath); inner.Kind == Broken {
				dep.Kind = Broken
			} else {
				dep.Formula = inner.Formula
			}
			return dep
		}
		dep.Kind = Broken
		return dep
	}

	return classifyAbsPath(depPath, kegPath, cellarPath)
}

// classifyAbsPath classifies an absolute library path.
func classifyAbsPath(p, kegPath, cellarPath string) Dep {
	dep := Dep{Path: p, Resolved: p}

	if isSystemLib(p) {
		dep.Kind = System
		return dep
	}

	kegPrefix := kegPath + string(filepath.Separator)
	if strings.HasPrefix(p, kegPrefix) {
		dep.Kind = Self
		return dep
	}

	cellarPrefix := cellarPath + string(filepath.Separator)
	if strings.HasPrefix(p, cellarPrefix) {
		// Extract formula name: Cellar/<name>/...
		rel := p[len(cellarPrefix):]
		if idx := strings.IndexByte(rel, filepath.Separator); idx > 0 {
			dep.Kind = OtherKeg
			dep.Formula = rel[:idx]
			return dep
		}
	}

	// Check if the path exists on disk at all.
	if _, err := os.Stat(p); err != nil {
		dep.Kind = Broken
		return dep
	}

	// Exists but not in cellar — could be from another package manager.
	// Treat as system.
	dep.Kind = System
	return dep
}

// resolveVariable attempts to resolve @rpath, @loader_path, or
// @executable_path references (macOS) and $ORIGIN (Linux).
func resolveVariable(depPath, binaryPath string, rpaths []string) string {
	binDir := filepath.Dir(binaryPath)

	switch {
	case strings.HasPrefix(depPath, "@loader_path/"):
		candidate := filepath.Join(binDir, depPath[len("@loader_path/"):])
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	case strings.HasPrefix(depPath, "@executable_path/"):
		candidate := filepath.Join(binDir, depPath[len("@executable_path/"):])
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	case strings.HasPrefix(depPath, "@rpath/"):
		suffix := depPath[len("@rpath/"):]
		for _, rp := range rpaths {
			// rpaths themselves can contain @loader_path
			resolved := rp
			if strings.HasPrefix(rp, "@loader_path/") {
				resolved = filepath.Join(binDir, rp[len("@loader_path/"):])
			} else if strings.HasPrefix(rp, "@executable_path/") {
				resolved = filepath.Join(binDir, rp[len("@executable_path/"):])
			}
			candidate := filepath.Join(resolved, suffix)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	case strings.HasPrefix(depPath, "$ORIGIN/"):
		candidate := filepath.Join(binDir, depPath[len("$ORIGIN/"):])
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func isVariableRef(p string) bool {
	return strings.HasPrefix(p, "@rpath/") ||
		strings.HasPrefix(p, "@loader_path/") ||
		strings.HasPrefix(p, "@executable_path/") ||
		strings.HasPrefix(p, "$ORIGIN/")
}

// isBinary checks magic bytes to detect Mach-O or ELF binaries.
func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}

	// Mach-O
	switch {
	case magic[0] == 0xCF && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE:
		return true
	case magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCF:
		return true
	case magic[0] == 0xCE && magic[1] == 0xFA && magic[2] == 0xED && magic[3] == 0xFE:
		return true
	case magic[0] == 0xFE && magic[1] == 0xED && magic[2] == 0xFA && magic[3] == 0xCE:
		return true
	case magic[0] == 0xCA && magic[1] == 0xFE && magic[2] == 0xBA && magic[3] == 0xBE:
		return true
	case magic[0] == 0xBE && magic[1] == 0xBA && magic[2] == 0xFE && magic[3] == 0xCA:
		return true
	}

	// ELF
	if magic[0] == 0x7F && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return true
	}

	return false
}

// isSystemLib reports whether the library path is an OS-provided library.
func isSystemLib(p string) bool {
	return isSystemLibPlatform(p)
}

// FormatOpts controls the output of FormatResult.
type FormatOpts struct {
	Test   bool
	Quiet  bool
	Strict *StrictResult // nil unless --strict was used
}

// FormatResult prints the linkage result in a human-readable format
// similar to Homebrew's output.
func FormatResult(r *Result, opts FormatOpts) string {
	if opts.Quiet {
		return formatQuiet(r, opts)
	}

	var b strings.Builder

	if opts.Test {
		broken := r.Broken()
		hasProblems := len(broken) > 0

		if opts.Strict != nil {
			hasProblems = hasProblems || len(opts.Strict.Undeclared) > 0 || len(opts.Strict.Unused) > 0
		}

		if !hasProblems {
			fmt.Fprintf(&b, "No broken linkage found for %s\n", r.Name)
			return b.String()
		}

		if len(broken) > 0 {
			fmt.Fprintf(&b, "Broken linkage in %s %s:\n", r.Name, r.Version)
			for _, d := range broken {
				fmt.Fprintf(&b, "  %s\n", d.Path)
			}
		}

		if opts.Strict != nil {
			if len(opts.Strict.Undeclared) > 0 {
				fmt.Fprintf(&b, "Undeclared dependencies:\n")
				for _, f := range opts.Strict.Undeclared {
					fmt.Fprintf(&b, "  %s\n", f)
				}
			}
			if len(opts.Strict.Unused) > 0 {
				fmt.Fprintf(&b, "Unused declared dependencies:\n")
				for _, f := range opts.Strict.Unused {
					fmt.Fprintf(&b, "  %s\n", f)
				}
			}
		}
		return b.String()
	}

	// Full report grouped by category.
	var system, self, other, variable, broken []Dep
	for _, br := range r.Binaries {
		for _, d := range br.Deps {
			switch d.Kind {
			case System:
				system = append(system, d)
			case Self:
				self = append(self, d)
			case OtherKeg:
				other = append(other, d)
			case Variable:
				variable = append(variable, d)
			case Broken:
				broken = append(broken, d)
			}
		}
	}

	printSection := func(title string, deps []Dep) {
		if len(deps) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s:\n", title)
		seen := make(map[string]bool)
		for _, d := range deps {
			key := d.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			if d.Formula != "" {
				fmt.Fprintf(&b, "  %s (provided by %s)\n", d.Path, d.Formula)
			} else if d.Resolved != "" && d.Resolved != d.Path {
				fmt.Fprintf(&b, "  %s => %s\n", d.Path, d.Resolved)
			} else {
				fmt.Fprintf(&b, "  %s\n", d.Path)
			}
		}
	}

	printSection("System libraries", system)
	printSection("Libraries from this keg", self)
	printSection("Libraries from other kegs", other)
	printSection("Variable-referenced libraries", variable)
	printSection("Broken dependencies", broken)

	if opts.Strict != nil {
		if len(opts.Strict.Undeclared) > 0 {
			fmt.Fprintf(&b, "Undeclared dependencies:\n")
			for _, f := range opts.Strict.Undeclared {
				fmt.Fprintf(&b, "  %s\n", f)
			}
		}
		if len(opts.Strict.Unused) > 0 {
			fmt.Fprintf(&b, "Unused declared dependencies:\n")
			for _, f := range opts.Strict.Unused {
				fmt.Fprintf(&b, "  %s\n", f)
			}
		}
	}

	if b.Len() == 0 {
		fmt.Fprintf(&b, "No dynamic libraries found in %s\n", r.Name)
	}

	return b.String()
}

// formatQuiet produces minimal output suitable for scripting.
// Only broken dep paths (and strict issues) are printed, one per line,
// with no headers or annotations.
func formatQuiet(r *Result, opts FormatOpts) string {
	var b strings.Builder
	seen := make(map[string]bool)

	for _, br := range r.Binaries {
		for _, d := range br.Deps {
			if d.Kind == Broken && !seen[d.Path] {
				seen[d.Path] = true
				fmt.Fprintln(&b, d.Path)
			}
		}
	}

	if opts.Strict != nil {
		for _, name := range opts.Strict.Undeclared {
			fmt.Fprintln(&b, name)
		}
		for _, name := range opts.Strict.Unused {
			fmt.Fprintln(&b, name)
		}
	}

	return b.String()
}

// ReverseEntry records that a formula links against a library from the target keg.
type ReverseEntry struct {
	Formula string // the formula that links against the target
	Binary  string // the binary in that formula
	Lib     string // the library path it links to in the target keg
}

// ReverseResult holds the reverse-linkage analysis.
type ReverseResult struct {
	Name    string
	Version string
	Entries []ReverseEntry
}

// isWithinBase reports whether child is equal to or contained within base.
// Both paths are expected to be absolute/cleaned.
func isWithinBase(base, child string) bool {
	rel, err := filepath.Rel(base, child)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// Reverse finds all installed formulas that dynamically link against
// libraries provided by the keg at kegPath.
func Reverse(name, version, kegPath, cellarPath string) (*ReverseResult, error) {
	result := &ReverseResult{
		Name:    name,
		Version: version,
	}

	if absKegPath, err := filepath.Abs(kegPath); err == nil {
		kegPath = filepath.Clean(absKegPath)
	} else {
		kegPath = filepath.Clean(kegPath)
	}
	kegPrefix := kegPath + string(filepath.Separator)

	// Normalize cellarPath, resolving symlinks for consistent comparison
	cellarRoot := cellarPath
	if resolved, err := filepath.EvalSymlinks(cellarRoot); err == nil {
		cellarRoot = filepath.Clean(resolved)
	} else {
		cellarRoot = filepath.Clean(cellarRoot)
	}
	if filepath.Base(cellarRoot) != "Cellar" {
		return result, nil
	}

	// Ensure cellarRoot is exactly the Cellar corresponding to kegPath's prefix.
	// This prevents scanning arbitrary user-controlled paths that merely end with "Cellar".
	kegVersionDir := filepath.Dir(kegPath)       // <prefix>/Cellar/<formula>
	kegFormulaDir := filepath.Dir(kegVersionDir) // <prefix>/Cellar
	expectedCellarRoot := filepath.Clean(kegFormulaDir)
	if cellarRoot != expectedCellarRoot {
		return result, nil
	}

	// Derive the final scan root from the validated expected path, then canonicalize it.
	// This keeps filesystem sinks off externally provided path values.
	scanRoot := expectedCellarRoot
	if resolved, err := filepath.EvalSymlinks(scanRoot); err == nil {
		scanRoot = filepath.Clean(resolved)
	}

	// Open scanRoot securely to prevent symlink bypass attacks
	rootHandle, err := os.OpenRoot(scanRoot)
	if err != nil {
		return result, nil
	}
	defer rootHandle.Close()

	entries, err := os.ReadDir(scanRoot)
	if err != nil {
		return result, nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		formulaName := entry.Name()
		if !validation.IsValidName(formulaName) || formulaName == name {
			continue
		}

		// Use symlink-safe lstat within the root to verify the entry is not a symlink
		// and is actually contained within cellarRoot
		formulaInfo, err := rootHandle.Lstat(formulaName)
		if err != nil {
			continue
		}
		// Reject symlinks to prevent path traversal
		if formulaInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !formulaInfo.IsDir() {
			continue
		}

		// Construct the absolute path for further operations
		formulaDir := filepath.Join(cellarRoot, formulaName)

		versions, err := os.ReadDir(formulaDir)
		if err != nil {
			continue
		}
		var versionName string
		for _, v := range versions {
			if v.IsDir() && validation.IsValidVersion(v.Name()) {
				versionName = v.Name()
				break
			}
		}
		if versionName == "" {
			continue
		}

		otherKegPath := filepath.Join(formulaDir, versionName)
		checkResult, err := Check(formulaName, versionName, otherKegPath, cellarRoot)
		if err != nil {
			continue
		}

		for _, br := range checkResult.Binaries {
			for _, dep := range br.Deps {
				ref := dep.Resolved
				if ref == "" {
					ref = dep.Path
				}
				if strings.HasPrefix(ref, kegPrefix) {
					result.Entries = append(result.Entries, ReverseEntry{
						Formula: formulaName,
						Binary:  br.Path,
						Lib:     ref,
					})
				}
			}
		}
	}

	return result, nil
}

// FormatReverseResult formats the reverse-linkage result for display.
func FormatReverseResult(r *ReverseResult, quiet bool) string {
	if len(r.Entries) == 0 {
		return fmt.Sprintf("No formulas link against %s\n", r.Name)
	}

	var b strings.Builder

	if quiet {
		seen := make(map[string]bool)
		var names []string
		for _, e := range r.Entries {
			if !seen[e.Formula] {
				seen[e.Formula] = true
				names = append(names, e.Formula)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintln(&b, n)
		}
		return b.String()
	}

	grouped := make(map[string][]ReverseEntry)
	var order []string
	for _, e := range r.Entries {
		if _, exists := grouped[e.Formula]; !exists {
			order = append(order, e.Formula)
		}
		grouped[e.Formula] = append(grouped[e.Formula], e)
	}
	sort.Strings(order)

	for _, fname := range order {
		fmt.Fprintf(&b, "%s:\n", fname)
		for _, e := range grouped[fname] {
			fmt.Fprintf(&b, "  %s => %s\n", e.Binary, e.Lib)
		}
	}

	return b.String()
}