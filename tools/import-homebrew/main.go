package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const apiURL = "https://formulae.brew.sh/api/formula.json"

// Homebrew JSON API structures (only fields we need).
type hbFormula struct {
	Name         string       `json:"name"`
	Desc         string       `json:"desc"`
	Homepage     string       `json:"homepage"`
	License      string       `json:"license"`
	Versions     hbVersions   `json:"versions"`
	Bottle       hbBottle     `json:"bottle"`
	Dependencies []string     `json:"dependencies"`
	KegOnly      bool         `json:"keg_only"`
	Deprecated   bool         `json:"deprecated"`
	Disabled     bool         `json:"disabled"`
}

type hbVersions struct {
	Stable string `json:"stable"`
}

type hbBottle struct {
	Stable hbBottleStable `json:"stable"`
}

type hbBottleStable struct {
	Files map[string]hbBottleFile `json:"files"`
}

type hbBottleFile struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Platform mapping: Homebrew platform key -> grew platform key.
// We pick the best (latest) macOS version available per arch.
var darwinARM64Prefs = []string{
	"arm64_tahoe", "arm64_sequoia", "arm64_sonoma", "arm64_ventura", "arm64_monterey",
}
var darwinAMD64Prefs = []string{
	"tahoe", "sequoia", "sonoma", "ventura", "monterey",
}
var linuxAMD64Prefs = []string{"x86_64_linux"}
var linuxARM64Prefs = []string{"arm64_linux"}

type platformMapping struct {
	grewKey string
	prefs   []string
}

var platforms = []platformMapping{
	{"darwin_arm64", darwinARM64Prefs},
	{"darwin_amd64", darwinAMD64Prefs},
	{"linux_amd64", linuxAMD64Prefs},
	{"linux_arm64", linuxARM64Prefs},
}

func pickBottle(files map[string]hbBottleFile, prefs []string) *hbBottleFile {
	for _, key := range prefs {
		if f, ok := files[key]; ok {
			return &f
		}
	}
	return nil
}

func yamlEscape(s string) string {
	if s == "" {
		return `""`
	}
	// Quote if contains special chars.
	if strings.ContainsAny(s, `:{}[]&*?|>!%@\'"#`) || strings.HasPrefix(s, " ") {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}

func generateYAML(f *hbFormula, urlMap, sha256Map map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", f.Name)
	fmt.Fprintf(&b, "version: %s\n", yamlEscape(f.Versions.Stable))
	fmt.Fprintf(&b, "description: %s\n", yamlEscape(f.Desc))
	fmt.Fprintf(&b, "homepage: %s\n", f.Homepage)
	if f.License != "" {
		fmt.Fprintf(&b, "license: %s\n", yamlEscape(f.License))
	}

	b.WriteString("url:\n")
	for _, pm := range platforms {
		if u, ok := urlMap[pm.grewKey]; ok {
			fmt.Fprintf(&b, "  %s: %s\n", pm.grewKey, u)
		}
	}

	b.WriteString("sha256:\n")
	for _, pm := range platforms {
		if s, ok := sha256Map[pm.grewKey]; ok {
			fmt.Fprintf(&b, "  %s: %s\n", pm.grewKey, s)
		}
	}

	b.WriteString("install:\n")
	b.WriteString("  type: archive\n")
	b.WriteString("  format: tar.gz\n")
	b.WriteString("  strip_components: 2\n")

	if len(f.Dependencies) > 0 {
		b.WriteString("dependencies:\n")
		for _, dep := range f.Dependencies {
			fmt.Fprintf(&b, "  - %s\n", dep)
		}
	}

	if f.KegOnly {
		b.WriteString("keg_only: true\n")
	}

	return b.String()
}

func main() {
	outDir := "taps/core"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	fmt.Fprintf(os.Stderr, "Fetching Homebrew formula index...\n")
	resp, err := http.Get(apiURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: fetch API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "error: API returned HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read body: %v\n", err)
		os.Exit(1)
	}

	var formulas []hbFormula
	if err := json.Unmarshal(data, &formulas); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Parsed %d formulas from Homebrew API\n", len(formulas))

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error: create output dir: %v\n", err)
		os.Exit(1)
	}

	imported := 0
	skipped := 0
	var importedNames []string

	for i := range formulas {
		f := &formulas[i]

		if f.Deprecated || f.Disabled {
			skipped++
			continue
		}

		if f.Versions.Stable == "" {
			skipped++
			continue
		}

		files := f.Bottle.Stable.Files
		if len(files) == 0 {
			skipped++
			continue
		}

		urlMap := map[string]string{}
		sha256Map := map[string]string{}

		for _, pm := range platforms {
			bottle := pickBottle(files, pm.prefs)
			if bottle != nil {
				urlMap[pm.grewKey] = bottle.URL
				sha256Map[pm.grewKey] = bottle.SHA256
			}
		}

		// Require at least one platform.
		if len(urlMap) == 0 {
			skipped++
			continue
		}

		yaml := generateYAML(f, urlMap, sha256Map)
		outPath := filepath.Join(outDir, f.Name+".yaml")
		if err := os.WriteFile(outPath, []byte(yaml), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write %s: %v\n", outPath, err)
			skipped++
			continue
		}

		imported++
		importedNames = append(importedNames, f.Name)
	}

	sort.Strings(importedNames)
	fmt.Fprintf(os.Stderr, "\nDone: %d formulas imported, %d skipped\n", imported, skipped)
	fmt.Fprintf(os.Stderr, "Output: %s/\n", outDir)
}
