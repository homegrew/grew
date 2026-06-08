package desc

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/homegrew/grew/pkg/context"
	"github.com/spf13/cobra"
)

var (
	descSearch      bool
	descName        bool
	descDescription bool
	descFormula     bool
	descCask        bool
	descPlain       bool
)

// maxPatternLen caps the length of a user-supplied regular expression to
// guard against pathological patterns.
const maxPatternLen = 1024

var Command = &cobra.Command{
	Use:   "desc [options] formula|cask|text|/regex/ [...]",
	Short: "Show package names and one-line descriptions",
	Long: `Display a package's name and one-line description.

With -s/--search, -n/--name, or --description, search formula and cask
names and/or descriptions for text. If the text is /flanked by slashes/, it
is treated as a regular expression.

By default, results are grouped under ==> Formulae and ==> Casks headers.
Use --plain to print plain "name: description" lines without headers.

Examples:
  grew desc jq
  grew desc --search json
  grew desc --name /^foo/
  grew desc --cask firefox`,
	RunE: func(c *cobra.Command, args []string) error {
		ctx, err := context.New()
		if err != nil {
			return err
		}
		return runDesc(ctx, args)
	},
}

func init() {
	Command.Flags().BoolVarP(&descSearch, "search", "s", false, "Search both names and descriptions for text. If text is /flanked by slashes/, it is treated as a regular expression.")
	Command.Flags().BoolVarP(&descName, "name", "n", false, "Search just names for text (regex if slash-flanked).")
	Command.Flags().BoolVar(&descDescription, "description", false, "Search just descriptions for text (regex if slash-flanked).")
	Command.Flags().BoolVar(&descFormula, "formula", false, "Treat all named arguments as formulae.")
	Command.Flags().BoolVar(&descFormula, "formulae", false, "Treat all named arguments as formulae.")
	Command.Flags().BoolVar(&descCask, "cask", false, "Treat all named arguments as casks.")
	Command.Flags().BoolVar(&descCask, "casks", false, "Treat all named arguments as casks.")
	Command.Flags().BoolVar(&descPlain, "plain", false, "Print plain 'name: description' lines without ==> Formulae / ==> Casks group headers.")
}

// entry is a single package name and its description.
type entry struct {
	name string
	desc string
}

func runDesc(ctx *context.Context, args []string) error {
	slog.Debug("starting desc command execution")

	searchModes := 0
	if descSearch {
		searchModes++
	}
	if descName {
		searchModes++
	}
	if descDescription {
		searchModes++
	}
	if searchModes > 1 {
		return fmt.Errorf("--search, --name, and --description are mutually exclusive")
	}

	if descFormula && descCask {
		return fmt.Errorf("--formula and --cask are mutually exclusive")
	}

	if len(args) == 0 {
		return fmt.Errorf("usage: grew desc [options] formula|cask|text|/regex/ [...]")
	}

	if searchModes == 1 {
		return runSearchMode(ctx, args)
	}
	return runNameMode(ctx, args)
}

// runNameMode resolves each argument as a package name and prints its
// description. Missing packages are reported to stderr but do not abort the
// remaining arguments; a non-nil error is returned at the end if any failed.
func runNameMode(ctx *context.Context, args []string) error {
	var formulae, casks []entry
	failed := false

	for _, name := range args {
		isCask, err := ctx.ResolveKind(name, descCask, descFormula)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			failed = true
			continue
		}
		if isCask {
			c, err := ctx.LoadCask(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				failed = true
				continue
			}
			casks = append(casks, entry{name: c.Name, desc: c.Description})
		} else {
			f, err := ctx.LoadFormula(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				failed = true
				continue
			}
			formulae = append(formulae, entry{name: f.Name, desc: f.Description})
		}
	}

	render(formulae, casks)

	if failed {
		return fmt.Errorf("some packages could not be found")
	}
	return nil
}

// runSearchMode loads all formulae and/or casks and matches them against the
// provided patterns.
func runSearchMode(ctx *context.Context, args []string) error {
	matchers, err := buildMatchers(args)
	if err != nil {
		return err
	}

	var formulae, casks []entry

	if !descCask {
		all, err := ctx.Loader.LoadAll()
		if err != nil {
			return err
		}
		for _, f := range all {
			if matchAny(matchers, f.Name, f.Description) {
				formulae = append(formulae, entry{name: f.Name, desc: f.Description})
			}
		}
	}

	if !descFormula {
		all, err := ctx.CaskLoader.LoadAll()
		if err != nil {
			return err
		}
		for _, c := range all {
			if matchAny(matchers, c.Name, c.Description) {
				casks = append(casks, entry{name: c.Name, desc: c.Description})
			}
		}
	}

	render(dedupe(formulae), dedupe(casks))
	return nil
}

// matcher tests a name and/or description for a single argument pattern.
type matcher struct {
	re      *regexp.Regexp // non-nil for /regex/ patterns
	literal string         // lowercased substring for plain text patterns
}

// matches reports whether the given text matches this pattern.
func (m matcher) matches(text string) bool {
	if m.re != nil {
		return m.re.MatchString(text)
	}
	return strings.Contains(strings.ToLower(text), m.literal)
}

func buildMatchers(args []string) ([]matcher, error) {
	matchers := make([]matcher, 0, len(args))
	for _, arg := range args {
		if len(arg) >= 2 && strings.HasPrefix(arg, "/") && strings.HasSuffix(arg, "/") {
			pattern := arg[1 : len(arg)-1]
			if len(pattern) > maxPatternLen {
				return nil, fmt.Errorf("regex too long (max %d characters): %q", maxPatternLen, arg)
			}
			re, err := regexp.Compile("(?i)" + pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid regex %q: %w", arg, err)
			}
			matchers = append(matchers, matcher{re: re})
		} else {
			matchers = append(matchers, matcher{literal: strings.ToLower(arg)})
		}
	}
	return matchers, nil
}

// matchAny reports whether any argument matcher matches according to the
// active search mode (name, description, or both).
func matchAny(matchers []matcher, name, description string) bool {
	for _, m := range matchers {
		switch {
		case descName:
			if m.matches(name) {
				return true
			}
		case descDescription:
			if m.matches(description) {
				return true
			}
		default: // descSearch: either
			if m.matches(name) || m.matches(description) {
				return true
			}
		}
	}
	return false
}

func dedupe(entries []entry) []entry {
	seen := make(map[string]bool, len(entries))
	out := entries[:0]
	for _, e := range entries {
		if seen[e.name] {
			continue
		}
		seen[e.name] = true
		out = append(out, e)
	}
	return out
}

// render prints the formula and cask entries either grouped (default) or as
// plain "name: description" lines (--plain). Both lists are sorted by name.
func render(formulae, casks []entry) {
	sortEntries(formulae)
	sortEntries(casks)

	if descPlain {
		for _, e := range formulae {
			fmt.Printf("%s: %s\n", e.name, e.desc)
		}
		for _, e := range casks {
			fmt.Printf("%s: %s\n", e.name, e.desc)
		}
		return
	}

	if len(formulae) > 0 {
		fmt.Println("==> Formulae")
		for _, e := range formulae {
			fmt.Printf("%s: %s\n", e.name, e.desc)
		}
	}
	if len(casks) > 0 {
		fmt.Println("==> Casks")
		for _, e := range casks {
			fmt.Printf("%s: %s\n", e.name, e.desc)
		}
	}
}

func sortEntries(entries []entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})
}
