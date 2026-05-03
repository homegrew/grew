package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/snapshot"
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

var ListCmd = &cobra.Command{
	Use:     "list [flags]",
	Aliases: []string{"ls"},
	Short:   "List installed formulas or casks",
	Long: `List all installed formulas with their versions.
With --cask, list installed casks instead.

Examples:
  grew list
  grew list --cask`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList(args)
	},
}

func init() {
	ListCmd.Flags().BoolVar(&listCask, "cask", false, "List installed casks.")
	ListCmd.Flags().BoolVar(&listFormulae, "formulae", false, "List installed formulas (default).")
	ListCmd.Flags().BoolVar(&listVersionsFlag, "versions", false, "Show all installed versions for each package.")
	ListCmd.Flags().BoolVar(&listMultiple, "multiple", false, "Only show packages with multiple versions installed.")
	ListCmd.Flags().BoolVarP(&listOnePerLine, "1", "1", false, "Print one entry per line, names only.")
	ListCmd.Flags().BoolVarP(&listLong, "l", "l", false, "Long format (name, version, path).")
	ListCmd.Flags().BoolVarP(&listByTime, "t", "t", false, "Sort by modification time (newest first).")
	ListCmd.Flags().BoolVarP(&listReverse, "r", "r", false, "Reverse sort order.")
	ListCmd.Flags().BoolVar(&listOnRequest, "installed-on-request", false, "Only show formulas explicitly installed by the user.")
	ListCmd.Flags().BoolVar(&listAsDep, "installed-as-dependency", false, "Only show formulas installed automatically as dependencies.")
	ListCmd.Flags().BoolVar(&listFullName, "full-name", false, "Show full keg path as name (tap/formula).")
	ListCmd.Flags().BoolVar(&listBuiltSrc, "built-from-source", false, "Only show formulas built from source.")
	ListCmd.Flags().BoolVar(&listPouredBottle, "poured-from-bottle", false, "Only show formulas poured from a pre-compiled bottle.")
	ListCmd.Flags().BoolVar(&listPinned, "pinned", false, "Only show pinned formulas.")
	rootCmd.AddCommand(ListCmd)
}

func runList(args []string) error {
	slog.Debug("starting list command execution")

	if listOnRequest && listAsDep {
		return fmt.Errorf("--installed-on-request and --installed-as-dependency are mutually exclusive")
	}
	if listBuiltSrc && listPouredBottle {
		return fmt.Errorf("--built-from-source and --poured-from-bottle are mutually exclusive")
	}

	if listCask && !listFormulae {
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
	if listOnRequest || listAsDep || listBuiltSrc || listPouredBottle {
		packages = filterByManifest(packages, cel, listOnRequest, listAsDep, listBuiltSrc, listPouredBottle)
		if len(packages) == 0 {
			fmt.Println("No matching formulas.")
			return nil
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
			return nil
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
			return nil
		}
	}

	if listVersionsFlag {
		return listVersions(cel, packages, listLong, listOnePerLine, listFullName, paths.Cellar)
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
