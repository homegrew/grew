// Command grew-genrepo is a unified tool for importing Homebrew formulas and casks
// into grew-compatible YAML definitions.
//
// Usage:
//
//	go run tools/grew-genrepo/main.go formula [output_dir]
//	go run tools/grew-genrepo/main.go cask [output_dir]
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/formula"
	"gopkg.in/yaml.v3"
)

const (
	formulaAPI = "https://formulae.brew.sh/api/formula.json"
	caskAPI    = "https://formulae.brew.sh/api/cask.json"
)

// Shared Platform Mapping Preferences
var (
	darwinARM64Prefs = []string{
		"arm64_tahoe", "arm64_sequoia", "arm64_sonoma", "arm64_ventura", "arm64_monterey", "arm64_big_sur",
	}
	darwinAMD64Prefs = []string{
		"tahoe", "sequoia", "sonoma", "ventura", "monterey", "big_sur", "catalina", "mojave",
	}
	linuxAMD64Prefs = []string{"x86_64_linux"}
	linuxARM64Prefs = []string{"arm64_linux"}

	platforms = []struct {
		key   string
		prefs []string
	}{
		{"darwin_arm64", darwinARM64Prefs},
		{"darwin_amd64", darwinAMD64Prefs},
		{"linux_amd64", linuxAMD64Prefs},
		{"linux_arm64", linuxARM64Prefs},
	}
)

func main() {
	opts := slog.HandlerOptions{Level: slog.LevelInfo}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &opts)))

	if len(os.Args) < 2 {
		usage()
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "formula":
		runFormulaImport(args)
	case "cask":
		runCaskImport(args)
	default:
		slog.Error("Unknown command", "command", cmd)
		usage()
	}
}

func usage() {
	fmt.Println("Usage: grew-genrepo <command> [args]")
	fmt.Println("\nCommands:")
	fmt.Println("  formula [output_dir]    Import Homebrew formulas (default: core)")
	fmt.Println("  cask [output_dir]       Import Homebrew casks (default: cask)")
	os.Exit(1)
}

// --- Formula Importer ---

type hbFormula struct {
	Name              string   `json:"name"`
	Desc              string   `json:"desc"`
	Homepage          string   `json:"homepage"`
	License           string   `json:"license"`
	Versions          struct { Stable string `json:"stable"` } `json:"versions"`
	Urls              struct { Stable struct { URL, Checksum string } `json:"stable"` } `json:"urls"`
	Bottle            struct { Stable struct { Files map[string]struct { URL, SHA256 string } `json:"files"` } `json:"stable"` } `json:"bottle"`
	Dependencies      []string `json:"dependencies"`
	BuildDependencies []string `json:"build_dependencies"`
	Variations        struct {
		LinuxAMD64 struct { Dependencies []string `json:"dependencies"` } `json:"x86_64_linux"`
		LinuxARM64 struct { Dependencies []string `json:"dependencies"` } `json:"arm64_linux"`
	} `json:"variations"`
	KegOnly    bool `json:"keg_only"`
	Deprecated bool `json:"deprecated"`
	Disabled   bool `json:"disabled"`
}

func runFormulaImport(args []string) {
	outDir := "core"
	if len(args) > 0 {
		outDir = args[0]
	}

	data := fetchAPI(formulaAPI)
	var hfs []hbFormula
	if err := json.Unmarshal(data, &hfs); err != nil {
		slog.Error("Parse JSON failed", "error", err)
		os.Exit(1)
	}

	os.MkdirAll(outDir, 0755)
	imported, skipped := 0, 0

	for _, hf := range hfs {
		if hf.Deprecated || hf.Disabled || hf.Versions.Stable == "" || len(hf.Bottle.Stable.Files) == 0 {
			skipped++
			continue
		}

		bottleMap := map[string]formula.BottleSpec{}
		for _, pm := range platforms {
			for _, pref := range pm.prefs {
				if f, ok := hf.Bottle.Stable.Files[pref]; ok {
					bottleMap[pm.key] = formula.BottleSpec{
						URL:    f.URL,
						SHA256: f.SHA256,
						// Homebrew API currently only provides SHA-256.
					}
					break
				}
			}
		}

		if len(bottleMap) == 0 {
			skipped++
			continue
		}

		f := &formula.Formula{
			Name:        hf.Name,
			Version:     hf.Versions.Stable,
			Description: hf.Desc,
			Homepage:    hf.Homepage,
			License:     hf.License,
			Bottle:      bottleMap,
			Source: formula.SourceSpec{
				URL:    hf.Urls.Stable.URL,
				SHA256: hf.Urls.Stable.Checksum,
			},
			Install: formula.InstallSpec{
				Type: "archive", Format: "tar.gz", StripComponents: 2,
			},
			Dependencies:      hf.Dependencies,
			BuildDependencies: hf.BuildDependencies,
			KegOnly:           hf.KegOnly,
		}

		linuxDeps := map[string]bool{}
		for _, d := range hf.Variations.LinuxAMD64.Dependencies { linuxDeps[d] = true }
		for _, d := range hf.Variations.LinuxARM64.Dependencies { linuxDeps[d] = true }
		if len(linuxDeps) > 0 {
			for d := range linuxDeps { f.LinuxDependencies = append(f.LinuxDependencies, d) }
			sort.Strings(f.LinuxDependencies)
		}

		saveYAML(filepath.Join(outDir, hf.Name+".yaml"), f)
		imported++
	}
	slog.Info("Formula import complete", "imported", imported, "skipped", skipped)
}

// --- Cask Importer ---

type hbCask struct {
	Token      string            `json:"token"`
	Name       []string          `json:"name"`
	Desc       string            `json:"desc"`
	Homepage   string            `json:"homepage"`
	License    string            `json:"license"`
	URL        string            `json:"url"`
	SHA256     string            `json:"sha256"`
	Version    string            `json:"version"`
	Artifacts  []json.RawMessage `json:"artifacts"`
	Variations map[string]struct { URL, SHA256 string } `json:"variations"`
	Deprecated bool              `json:"deprecated"`
	Disabled   bool              `json:"disabled"`
}

func runCaskImport(args []string) {
	outDir := "cask"
	if len(args) > 0 {
		outDir = args[0]
	}

	data := fetchAPI(caskAPI)
	var hcs []hbCask
	if err := json.Unmarshal(data, &hcs); err != nil {
		slog.Error("Parse JSON failed", "error", err)
		os.Exit(1)
	}

	os.MkdirAll(outDir, 0755)
	imported, skipped := 0, 0

	for _, hc := range hcs {
		if hc.Deprecated || hc.Disabled || hc.Version == "" || hc.Version == "latest" || hc.SHA256 == "" || hc.SHA256 == "no_check" {
			skipped++
			continue
		}

		urlMap := map[string]string{"darwin_arm64": hc.URL, "darwin_amd64": hc.URL}
		shaMap := map[string]string{"darwin_arm64": hc.SHA256, "darwin_amd64": hc.SHA256}

		for _, pm := range platforms {
			if !strings.HasPrefix(pm.key, "darwin") { continue }
			for _, pref := range pm.prefs {
				if v, ok := hc.Variations[pref]; ok {
					urlMap[pm.key] = v.URL
					shaMap[pm.key] = v.SHA256
					break
				}
			}
		}

		arts := parseCaskArtifacts(hc.Artifacts)
		if len(arts.App) == 0 && len(arts.Bin) == 0 {
			skipped++
			continue
		}

		c := &cask.Cask{
			Name: hc.Token, Version: hc.Version, Description: hc.Desc, Homepage: hc.Homepage,
			License: hc.License, URL: urlMap, SHA256: shaMap, Artifacts: arts,
			Source: cask.SourceSpec{URL: hc.URL, SHA256: hc.SHA256},
		}

		saveYAML(filepath.Join(outDir, hc.Token+".yaml"), c)
		imported++
	}
	slog.Info("Cask import complete", "imported", imported, "skipped", skipped)
}

func parseCaskArtifacts(raw []json.RawMessage) cask.Artifacts {
	var res cask.Artifacts
	for _, m := range raw {
		var obj map[string]json.RawMessage
		json.Unmarshal(m, &obj)
		if app, ok := obj["app"]; ok {
			var apps []string
			json.Unmarshal(app, &apps)
			res.App = append(res.App, apps...)
		}
		if bin, ok := obj["binary"]; ok {
			var binArr []json.RawMessage
			if err := json.Unmarshal(bin, &binArr); err == nil && len(binArr) > 0 {
				var path string
				json.Unmarshal(binArr[0], &path)
				name := filepath.Base(path)
				if len(binArr) > 1 {
					var opts map[string]string
					if err := json.Unmarshal(binArr[1], &opts); err == nil {
						if t, ok := opts["target"]; ok { name = t }
					}
				}
				res.Bin = append(res.Bin, name)
			}
		}
	}
	return res
}

// --- Common Helpers ---

func fetchAPI(url string) []byte {
	slog.Info("Fetching API", "url", url)
	resp, err := http.Get(url)
	if err != nil {
		slog.Error("Fetch failed", "error", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		slog.Error("API error", "status", resp.Status)
		os.Exit(1)
	}
	data, _ := io.ReadAll(resp.Body)
	return data
}

func saveYAML(path string, v any) {
	data, err := yaml.Marshal(v)
	if err != nil {
		slog.Warn("Marshal failed", "path", path, "error", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Warn("Write failed", "path", path, "error", err)
	}
}
