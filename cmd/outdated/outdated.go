package outdated

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/flags"
	pkgversion "github.com/homegrew/grew/pkg/version"
	"github.com/spf13/cobra"
)

var (
	formulaOnly bool
	caskOnly    bool
	jsonOutput  bool
	minVersion  string
)

// Command is the cobra command for "grew outdated".
var Command = &cobra.Command{
	Use:   "outdated [formula|cask ...]",
	Short: "List installed casks and formulae that have an updated version available",
	Long: `List installed casks and formulae that have an updated version available. By
default, version information is displayed in interactive shells and suppressed
otherwise.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(c *cobra.Command, args []string) error {
		ctx, err := context.New()
		if err != nil {
			return err
		}

		return runOutdated(ctx, args)
	},
}

func init() {
	Command.Flags().BoolVar(&formulaOnly, "formula", false, "List only outdated formulae.")
	Command.Flags().BoolVar(&formulaOnly, "formulae", false, "List only outdated formulae.")
	Command.Flags().BoolVar(&caskOnly, "cask", false, "List only outdated casks.")
	Command.Flags().BoolVar(&caskOnly, "casks", false, "List only outdated casks.")
	Command.Flags().BoolVar(&jsonOutput, "json", false, "Print output in JSON format.")
	Command.Flags().StringVar(&minVersion, "minimum-version", "", "Only list a named formula or cask with an installed version below the given minimum version.")
	Command.Flags().StringVar(&minVersion, "min-version", "", "Only list a named formula or cask with an installed version below the given minimum version.")

	_ = Command.Flags().MarkHidden("formulae")
	_ = Command.Flags().MarkHidden("casks")
	_ = Command.Flags().MarkHidden("min-version")
}

type outdatedEntry struct {
	Name      string
	Installed string
	Available string
	Kind      string // "formula" or "cask"
	Pinned    bool
}

func runOutdated(ctx *context.Context, args []string) error {
	slog.Debug("starting outdated command execution")

	// Build optional name filter from positional args.
	nameFilter := make(map[string]bool, len(args))
	for _, a := range args {
		nameFilter[a] = true
	}
	hasFilter := len(nameFilter) > 0

	var results []outdatedEntry

	// Formula pass — skip if --cask is explicitly set without --formula.
	if !caskOnly || formulaOnly {
		installed, err := ctx.Cellar.List()
		if err != nil {
			return err
		}
		for _, pkg := range installed {
			if hasFilter && !nameFilter[pkg.Name] {
				continue
			}
			f, err := ctx.LoadFormula(pkg.Name)
			if err != nil {
				slog.Debug(fmt.Sprintf("outdated: skipping formula %s: could not load definition (%v)", pkg.Name, err))
				continue
			}
			if pkg.Version == f.Version {
				continue
			}
			if minVersion != "" && pkgversion.Compare(pkg.Version, minVersion) < 0 {
				continue
			}
			results = append(results, outdatedEntry{
				Name:      pkg.Name,
				Installed: pkg.Version,
				Available: f.Version,
				Kind:      "formula",
				Pinned:    ctx.Cellar.IsPinned(pkg.Name),
			})
		}
	}

	// Cask pass — skip if --formula is explicitly set without --cask.
	if !formulaOnly || caskOnly {
		casks, err := ctx.Caskroom.List()
		if err != nil {
			return err
		}
		for _, c := range casks {
			if hasFilter && !nameFilter[c.Name] {
				continue
			}
			def, err := ctx.LoadCask(c.Name)
			if err != nil {
				slog.Debug(fmt.Sprintf("outdated: skipping cask %s: not in any tap (%v)", c.Name, err))
				continue
			}
			if c.Version == def.Version {
				continue
			}
			if minVersion != "" && pkgversion.Compare(c.Version, minVersion) < 0 {
				continue
			}
			results = append(results, outdatedEntry{
				Name:      c.Name,
				Installed: c.Version,
				Available: def.Version,
				Kind:      "cask",
			})
		}
	}

	if jsonOutput {
		return printJSON(results)
	}

	if len(results) == 0 {
		if !flags.Quiet {
			fmt.Println("Everything is up-to-date.")
		}
		return nil
	}

	for _, e := range results {
		if flags.Quiet {
			fmt.Println(e.Name)
			continue
		}
		pinMarker := ""
		if e.Pinned {
			pinMarker = " [pinned]"
		}
		if flags.Verbose {
			fmt.Fprintf(os.Stderr, "%-20s %s -> %s%s (%s)\n", e.Name, e.Installed, e.Available, pinMarker, e.Kind)
		} else {
			fmt.Fprintf(os.Stderr, "%-20s %s -> %s%s\n", e.Name, e.Installed, e.Available, pinMarker)
		}
	}
	return nil
}

type outdatedJSON struct {
	Formulae []outdatedJSONEntry `json:"formulae"`
	Casks    []outdatedJSONEntry `json:"casks"`
}

type outdatedJSONEntry struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installed_versions"`
	CurrentVersion   string `json:"current_version"`
	Pinned           bool   `json:"pinned,omitempty"`
}

func printJSON(entries []outdatedEntry) error {
	out := outdatedJSON{
		Formulae: []outdatedJSONEntry{},
		Casks:    []outdatedJSONEntry{},
	}
	for _, e := range entries {
		je := outdatedJSONEntry{
			Name:             e.Name,
			InstalledVersion: e.Installed,
			CurrentVersion:   e.Available,
			Pinned:           e.Pinned,
		}
		if e.Kind == "cask" {
			out.Casks = append(out.Casks, je)
		} else {
			out.Formulae = append(out.Formulae, je)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
