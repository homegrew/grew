package list

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/homegrew/grew/pkg/cellar"
	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/version"
	"github.com/spf13/cobra"
)

var (
	listCask         bool
	listFormulae     bool
	listVersionsFlag bool
	listMultiple     bool
	listOnePerLine   bool
	listLong         bool
	listByTime       bool
	listReverse      bool
	listOnRequest    bool
	listAsDep        bool
	listFullName     bool
	listBuiltSrc     bool
	listPouredBottle bool
	listPinned       bool
)

var Command = &cobra.Command{
	Use:     "list [flags]",
	Aliases: []string{"ls"},
	Short:   "List installed formulas and casks",
	Long: `List all installed formulas and casks with their versions.
By default both kinds are listed. Use --formula or --cask to restrict.

Examples:
  grew list
  grew list --cask`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := context.New()
		if err != nil {
			return err
		}
		return runList(ctx, args)
	},
}

func init() {
	Command.Flags().BoolVar(&listCask, "cask", false, "List installed casks.")
	Command.Flags().BoolVar(&listFormulae, "formula", false, "List installed formulas (default).")
	Command.Flags().BoolVar(&listVersionsFlag, "versions", false, "Show all installed versions for each package.")
	Command.Flags().BoolVar(&listMultiple, "multiple", false, "Only show packages with multiple versions installed.")
	Command.Flags().BoolVarP(&listOnePerLine, "1", "1", false, "Print one entry per line, names only.")
	Command.Flags().BoolVarP(&listLong, "l", "l", false, "Long format (name, version, path).")
	Command.Flags().BoolVarP(&listByTime, "t", "t", false, "Sort by modification time (newest first).")
	Command.Flags().BoolVarP(&listReverse, "r", "r", false, "Reverse sort order.")
	Command.Flags().BoolVar(&listOnRequest, "installed-on-request", false, "Only show formulas explicitly installed by the user.")
	Command.Flags().BoolVar(&listAsDep, "installed-as-dependency", false, "Only show formulas installed automatically as dependencies.")
	Command.Flags().BoolVar(&listFullName, "full-name", false, "Show full keg path as name (tap/formula).")
	Command.Flags().BoolVar(&listBuiltSrc, "built-from-source", false, "Only show formulas built from source.")
	Command.Flags().BoolVar(&listPouredBottle, "poured-from-bottle", false, "Only show formulas poured from a pre-compiled bottle.")
	Command.Flags().BoolVar(&listPinned, "pinned", false, "Only show pinned formulas.")
}

func runList(ctx *context.Context, _ []string) error {
	slog.Debug("starting list command execution")

	if listOnRequest && listAsDep {
		return fmt.Errorf("--installed-on-request and --installed-as-dependency are mutually exclusive")
	}
	if listBuiltSrc && listPouredBottle {
		return fmt.Errorf("--built-from-source and --poured-from-bottle are mutually exclusive")
	}

	// Decide which kinds to list. With neither flag, list both.
	showCasks := listCask || (!listCask && !listFormulae)
	showFormulae := listFormulae || (!listCask && !listFormulae)

	// Formula-specific filters/formats have no meaning for casks. If any is
	// set, treat it as an implicit formulae-only query so we never print casks
	// that would silently ignore the user's filter.
	formulaOnlyFlag := listVersionsFlag || listMultiple || listLong || listByTime ||
		listReverse || listOnRequest || listAsDep || listFullName ||
		listBuiltSrc || listPouredBottle || listPinned
	if formulaOnlyFlag && !listCask {
		showCasks = false
	}

	// handled tracks whether anything was written to stdout — a listed package
	// or a filter-specific empty-state message. The generic "No packages
	// installed." fallback fires only when both sections stayed silent.
	handled := false
	if showFormulae {
		ok, err := listFormulaePackages(ctx)
		if err != nil {
			return err
		}
		handled = handled || ok
	}

	if showCasks {
		casks, err := ctx.Caskroom.ListInstalled()
		if err != nil {
			return err
		}
		for _, c := range casks {
			if listOnePerLine {
				fmt.Println(c.Name)
			} else {
				fmt.Printf("%-20s %s\n", c.Name, c.Version)
			}
		}
		handled = handled || len(casks) > 0
	}

	if !handled {
		fmt.Println("No packages installed.")
	}
	return nil
}

// listFormulaePackages runs the full formula listing pipeline (filters, sorts,
// and formats) and prints the result. It reports whether it produced any
// output — either listed formulas or a filter-specific empty-state message
// (e.g. "No pinned formulas.") — so the caller can decide whether to print the
// generic "No packages installed." fallback.
func listFormulaePackages(ctx *context.Context) (bool, error) {
	paths := ctx.Paths
	cel := ctx.Cellar

	packages, err := cel.List()
	if err != nil {
		return false, err
	}
	if len(packages) == 0 {
		return false, nil
	}

	// Filter by install reason or build method using snapshot metadata.
	if listOnRequest || listAsDep || listBuiltSrc || listPouredBottle {
		packages = cel.FilterByManifest(packages, listOnRequest, listAsDep, listBuiltSrc, listPouredBottle)
		if len(packages) == 0 {
			fmt.Println("No matching formulas.")
			return true, nil
		}
	}

	if listPinned {
		var pinnedPkgs []cellar.InstalledPackage
		for _, p := range packages {
			if cel.IsPinned(p.Name) {
				pinnedPkgs = append(pinnedPkgs, p)
			}
		}
		packages = pinnedPkgs
		if len(packages) == 0 {
			fmt.Println("No pinned formulas.")
			return true, nil
		}
	}

	if listByTime {
		sortByTime(packages)
	}

	if listReverse {
		reversePackages(packages)
	}

	if listMultiple {
		packages = filterMultiple(cel, packages)
		if len(packages) == 0 {
			fmt.Println("No formulas with multiple versions installed.")
			return true, nil
		}
	}

	if listVersionsFlag {
		if err := listVersions(cel, packages, listLong, listOnePerLine, listFullName, paths.Cellar); err != nil {
			return false, err
		}
		return len(packages) > 0, nil
	}

	for _, p := range packages {
		name := p.Name
		if listFullName {
			name = filepath.Join(paths.Cellar, p.Name, p.Version)
		}
		switch {
		case listOnePerLine:
			fmt.Println(name)
		case listLong:
			fmt.Printf("%-20s %-12s %s\n", name, p.Version, p.Path)
		default:
			fmt.Printf("%-20s %s\n", name, p.Version)
		}
	}
	return len(packages) > 0, nil
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
			fmt.Printf("%-20s %s\n", name, version.Join(vers))
		}
	})
	return nil
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
	if err := safepath.SafeAbsolutePath(path); err != nil {
		return time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
