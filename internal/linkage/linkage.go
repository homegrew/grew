// Package linkage inspects dynamic library dependencies of installed kegs
// and classifies them as system, self, other-formula, or broken.
package linkage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DepKind classifies a dynamic library dependency.
type DepKind int

const (
	System    DepKind = iota // OS-provided (/usr/lib, /System/Library, etc.)
	Self                     // provided by the keg itself
	OtherKeg                 // provided by another formula in the Cellar
	Variable                 // uses @rpath, @loader_path, @executable_path, or $ORIGIN
	Broken                   // cannot be resolved on disk
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
	Path     string // raw path as embedded in the binary
	Kind     DepKind
	Resolved string // resolved path on disk (empty if unresolvable)
	Formula  string // formula name if Kind == OtherKeg
}

// BinaryResult holds the linkage analysis for one binary file.
type BinaryResult struct {
	Path string
	Deps []Dep
}

// Result holds the full linkage analysis for a keg.
type Result struct {
	Name     string
	Version  string
	KegPath  string
	Binaries []BinaryResult
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

// FormatResult prints the linkage result in a human-readable format
// similar to Homebrew's output.
func FormatResult(r *Result, test bool) string {
	var b strings.Builder

	if test {
		broken := r.Broken()
		if len(broken) == 0 {
			fmt.Fprintf(&b, "No broken linkage found for %s\n", r.Name)
			return b.String()
		}
		fmt.Fprintf(&b, "Broken linkage in %s %s:\n", r.Name, r.Version)
		for _, d := range broken {
			fmt.Fprintf(&b, "  %s\n", d.Path)
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

	if b.Len() == 0 {
		fmt.Fprintf(&b, "No dynamic libraries found in %s\n", r.Name)
	}

	return b.String()
}
