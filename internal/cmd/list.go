package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/snapshot"
)

func runList(args []string) error {
	slog.Debug("starting list command execution")
	slog.Debug("starting list command execution")
	fs := flag.NewFlagSet("list", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: grew list [options]

List all installed formulas or casks. Aliases: ls

Options:
  --cask                     List installed casks.
  --formulae                 List installed formulas (default).
  --versions                 Show all installed versions for each package.
  --multiple                 Only show packages with multiple versions installed.
  -1                         Print one entry per line, names only.
  -l                         Long format (name, version, path).
  -t                         Sort by modification time (newest first).
  -r                         Reverse sort order.
  --installed-on-request     Only show formulas explicitly installed by the user.
  --installed-as-dependency  Only show formulas installed automatically as dependencies.
  --full-name                Show full keg path as name (tap/formula).
  --built-from-source        Only show formulas built from source.
  --poured-from-bottle       Only show formulas poured from a pre-compiled bottle.
  --pinned                   Only show pinned formulas.
  -v, --verbose              Show detailed output.
  -d, --debug                Show debug diagnostics (implies --verbose).
`)
	}

	flags.Register(fs)
	isCask := fs.Bool("cask", false, "List installed casks")
	isFormulae := fs.Bool("formulae", false, "List installed formulas (default)")
	versions := fs.Bool("versions", false, "Show all installed versions")
	multiple := fs.Bool("multiple", false, "Only show formulas with multiple versions")
	onePerLine := fs.Bool("1", false, "One entry per line, names only")
	long := fs.Bool("l", false, "Long format (name, version, path)")
	byTime := fs.Bool("t", false, "Sort by modification time (newest first)")
	reverse := fs.Bool("r", false, "Reverse sort order")
	onRequest := fs.Bool("installed-on-request", false, "Only show formulas installed on request")
	asDep := fs.Bool("installed-as-dependency", false, "Only show formulas installed as dependencies")
	fullName := fs.Bool("full-name", false, "Show full keg path as name (tap/formula)")
	builtSrc := fs.Bool("built-from-source", false, "Only show formulas built from source") //nolint:revive
	pouredBottle := fs.Bool("poured-from-bottle", false, "Only show formulas poured from bottle")
	pinned := fs.Bool("pinned", false, "Only show pinned formulas")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if *onRequest && *asDep {
		return fmt.Errorf("--installed-on-request and --installed-as-dependency are mutually exclusive")
	}
	if *builtSrc && *pouredBottle {
		return fmt.Errorf("--built-from-source and --poured-from-bottle are mutually exclusive")
	}

	if *isCask && !*isFormulae {
		return caskList()
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}

	packages, err := cel.List()
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}

	// Filter by install reason or build method using snapshot metadata.
	if *onRequest || *asDep || *builtSrc || *pouredBottle {
		packages = filterByManifest(packages, cel, *onRequest, *asDep, *builtSrc, *pouredBottle)
		if len(packages) == 0 {
			fmt.Println("No matching formulas.")
			return nil
		}
	}

	if *pinned {
		var pinnedPkgs []cellar.InstalledPackage
		for _, p := range packages {
			if cel.IsPinned(p.Name) {
				pinnedPkgs = append(pinnedPkgs, p)
			}
		}
		packages = pinnedPkgs
		if len(packages) == 0 {
			fmt.Println("No pinned formulas.")
			return nil
		}
	}

	if *byTime {
		sortByTime(packages)
	}

	if *reverse {
		reversePackages(packages)
	}

	if *multiple {
		packages = filterMultiple(cel, packages)
		if len(packages) == 0 {
			fmt.Println("No formulas with multiple versions installed.")
			return nil
		}
	}

	if *versions {
		return listVersions(cel, packages, *long, *onePerLine, *fullName, paths.Cellar)
	}

	for _, p := range packages {
		name := p.Name
		if *fullName {
			name = filepath.Join(paths.Cellar, p.Name, p.Version)
		}
		switch {
		case *onePerLine:
			fmt.Println(name)
		case *long:
			fmt.Printf("%-20s %-12s %s\n", name, p.Version, p.Path)
		default:
			fmt.Printf("%-20s %s\n", name, p.Version)
		}
	}
	return nil
}

// filterByManifest filters packages based on snapshot manifest metadata.
func filterByManifest(packages []cellar.InstalledPackage, cel *cellar.Cellar, onRequest, asDep, builtSrc, pouredBottle bool) []cellar.InstalledPackage {
	var result []cellar.InstalledPackage
	for _, p := range packages {
		kegPath, err := cel.KegPath(p.Name, p.Version)
		if err != nil {
			continue
		}
		m, err := snapshot.Load(kegPath)
		if err != nil {
			// No manifest — can't determine install reason. Skip for these filters.
			continue
		}

		if onRequest && !m.InstalledOnRequest {
			continue
		}
		if asDep && m.InstalledOnRequest {
			continue
		}
		if builtSrc && !m.BuiltFromSource {
			continue
		}
		if pouredBottle && m.BuiltFromSource {
			continue
		}

		result = append(result, p)
	}
	return result
}

// eachUniquePackage iterates over unique packages and calls fn with the package and its versions.
func eachUniquePackage(cel *cellar.Cellar, packages []cellar.InstalledPackage, fn func(p cellar.InstalledPackage, vers []string)) {
	seen := make(map[string]bool)
	for _, p := range packages {
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true

		vers, err := cel.InstalledVersions(p.Name)
		if err != nil {
			continue
		}
		fn(p, vers)
	}
}

// listVersions prints all installed versions for each formula.
func listVersions(cel *cellar.Cellar, packages []cellar.InstalledPackage, long, onePerLine, fullName bool, cellarPath string) error {
	eachUniquePackage(cel, packages, func(p cellar.InstalledPackage, vers []string) {
		name := p.Name
		if fullName {
			name = filepath.Join(cellarPath, p.Name)
		}

		if onePerLine {
			for _, v := range vers {
				fmt.Printf("%s@%s\n", name, v)
			}
		} else if long {
			for _, v := range vers {
				keg, err := cel.KegPath(p.Name, v)
				if err != nil {
					continue
				}
				fmt.Printf("%-20s %-12s %s\n", name, v, keg)
			}
		} else {
			fmt.Printf("%-20s %s\n", name, joinVersions(vers))
		}
	})
	return nil
}

func joinVersions(vers []string) string {
	if len(vers) == 1 {
		return vers[0]
	}
	s := vers[0]
	for _, v := range vers[1:] {
		s += ", " + v
	}
	return s
}

// filterMultiple returns only packages that have more than one version installed.
func filterMultiple(cel *cellar.Cellar, packages []cellar.InstalledPackage) []cellar.InstalledPackage {
	var result []cellar.InstalledPackage
	eachUniquePackage(cel, packages, func(p cellar.InstalledPackage, vers []string) {
		if len(vers) > 1 {
			result = append(result, p)
		}
	})
	return result
}

// sortByTime sorts packages by keg modification time, newest first.
func sortByTime(packages []cellar.InstalledPackage) {
	sort.Slice(packages, func(i, j int) bool {
		return kegModTime(packages[i].Path).After(kegModTime(packages[j].Path))
	})
}

func reversePackages(packages []cellar.InstalledPackage) {
	for i, j := 0, len(packages)-1; i < j; i, j = i+1, j-1 {
		packages[i], packages[j] = packages[j], packages[i]
	}
}

func kegModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
