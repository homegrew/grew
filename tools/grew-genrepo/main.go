// Command grew-genrepo is a unified tool for importing Homebrew formulas and casks
// into grew-compatible YAML definitions.
//
// Usage:
//
//	go run tools/grew-genrepo/main.go formula [output_dir]
//	go run tools/grew-genrepo/main.go cask [output_dir]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/pkg/logger"
	"gopkg.in/yaml.v3"
)

const (
	formulaAPI = "https://formulae.brew.sh/api/formula.json"
	caskAPI    = "https://formulae.brew.sh/api/cask.json"
)

// Shared Platform Mapping Preferences
var (
	darwinARM64Prefs = []string{
		"arm64_tahoe", "arm64_sequoia", "arm64_sonoma", "arm64_ventura", "arm64_monterey", "arm64_big_sur", "all",
	}
	darwinAMD64Prefs = []string{
		"tahoe", "sequoia", "sonoma", "ventura", "monterey", "big_sur", "catalina", "mojave", "all",
	}
	linuxAMD64Prefs = []string{"x86_64_linux", "all"}
	linuxARM64Prefs = []string{"arm64_linux", "all"}

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
	fs := flag.NewFlagSet("grew-genrepo", flag.ExitOnError)
	var verbose, debug bool
	fs.BoolVar(&verbose, "v", false, "Verbose output")
	fs.BoolVar(&debug, "debug", false, "Debug output (implies verbose)")

	fs.Usage = usage
	if err := fs.Parse(os.Args[1:]); err != nil {
		usage()
	}

	if debug {
		verbose = true
	}

	logger.Init(verbose, debug)

	args := fs.Args()
	if len(args) < 1 {
		usage()
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "formula":
		runFormulaImport(cmdArgs)
	case "cask":
		runCaskImport(cmdArgs)
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
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Homepage string `json:"homepage"`
	License  string `json:"license"`
	Versions struct {
		Stable string `json:"stable"`
	} `json:"versions"`
	Urls struct {
		Stable struct{ URL, Checksum string } `json:"stable"`
	} `json:"urls"`
	Bottle struct {
		Stable struct {
			Files map[string]struct{ URL, SHA256 string } `json:"files"`
		} `json:"stable"`
	} `json:"bottle"`
	Dependencies      []string `json:"dependencies"`
	BuildDependencies []string `json:"build_dependencies"`
	Variations        struct {
		LinuxAMD64 struct {
			Dependencies []string `json:"dependencies"`
		} `json:"x86_64_linux"`
		LinuxARM64 struct {
			Dependencies []string `json:"dependencies"`
		} `json:"arm64_linux"`
	} `json:"variations"`
	KegOnly    bool `json:"keg_only"`
	Deprecated bool `json:"deprecated"`
	Disabled   bool `json:"disabled"`
}

var safeOutDirName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func sanitizeOutputDir(input string) (string, error) {
	v := strings.TrimSpace(input)
	if v == "" {
		return "", fmt.Errorf("output_dir must not be empty")
	}
	if filepath.IsAbs(v) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	if strings.Contains(v, "/") || strings.Contains(v, "\\") || strings.Contains(v, "..") {
		return "", fmt.Errorf("output_dir must be a single directory name")
	}
	if !safeOutDirName.MatchString(v) {
		return "", fmt.Errorf("output_dir contains invalid characters")
	}
	return v, nil
}

func runFormulaImport(args []string) {
	outDir := "core"
	if len(args) > 0 {
		safeDir, err := sanitizeOutputDir(args[0])
		if err != nil {
			slog.Error("Invalid output_dir", "value", args[0], "error", err)
			os.Exit(1)
		}
		outDir = safeDir
	}
	resolvedOutDir, err := safeOutputDir(outDir)
	if err != nil {
		slog.Error("Invalid output directory", "output_dir", outDir, "error", err)
		os.Exit(1)
	}

	data := fetchAPI(formulaAPI)
	var hfs []hbFormula
	if err := json.Unmarshal(data, &hfs); err != nil {
		slog.Error("Parse JSON failed", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(resolvedOutDir, 0755); err != nil {
		slog.Error("Create output directory failed", "dir", resolvedOutDir, "error", err)
		os.Exit(1)
	}
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
		for _, d := range hf.Variations.LinuxAMD64.Dependencies {
			linuxDeps[d] = true
		}
		for _, d := range hf.Variations.LinuxARM64.Dependencies {
			linuxDeps[d] = true
		}
		if len(linuxDeps) > 0 {
			for d := range linuxDeps {
				f.LinuxDependencies = append(f.LinuxDependencies, d)
			}
			sort.Strings(f.LinuxDependencies)
		}

		outPath, err := safeJoinUnderBase(resolvedOutDir, hf.Name+".yaml")
		if err != nil {
			slog.Warn("Skipping formula due to invalid output path", "name", hf.Name, "error", err)
			skipped++
			continue
		}
		saveYAML(outPath, f)
		imported++
	}
	slog.Info("Formula import complete", "imported", imported, "skipped", skipped)
}

// --- Cask Importer ---

type hbCask struct {
	Token      string                                          `json:"token"`
	Name       []string                                        `json:"name"`
	Desc       string                                          `json:"desc"`
	Homepage   string                                          `json:"homepage"`
	License    string                                          `json:"license"`
	URL        string                                          `json:"url"`
	SHA256     string                                          `json:"sha256"`
	SHA512     string                                          `json:"sha512"`
	Version    string                                          `json:"version"`
	Artifacts  []json.RawMessage                               `json:"artifacts"`
	Variations map[string]struct{ URL, SHA256, SHA512 string } `json:"variations"`
	Deprecated bool                                            `json:"deprecated"`
	Disabled   bool                                            `json:"disabled"`
}

func safeOutputDir(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", fmt.Errorf("output directory cannot be empty")
	}

	baseDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute working directory: %w", err)
	}

	targetPath := filepath.Clean(input)
	targetAbs, err := filepath.Abs(filepath.Join(baseDir, targetPath))
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}

	rel, err := filepath.Rel(baseDir, targetAbs)
	if err != nil {
		return "", fmt.Errorf("check output directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("output directory must be within %s", baseDir)
	}

	return targetAbs, nil
}

func runCaskImport(args []string) {
	outDirInput := "cask"
	if len(args) > 0 {
		outDirInput = args[0]
	}

	outDir, err := safeOutputDir(outDirInput)
	if err != nil {
		slog.Error("Invalid output directory", "error", err, "outDir", outDirInput)
		os.Exit(1)
	}

	data := fetchAPI(caskAPI)
	var hcs []hbCask
	if err := json.Unmarshal(data, &hcs); err != nil {
		slog.Error("Parse JSON failed", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		slog.Error("Create output directory failed", "dir", outDir, "error", err)
		os.Exit(1)
	}
	imported, skipped := 0, 0

	for _, hc := range hcs {
		if hc.Deprecated || hc.Disabled || hc.Version == "" || hc.Version == "latest" || (hc.SHA256 == "" && hc.SHA512 == "") || hc.SHA256 == "no_check" {
			skipped++
			continue
		}
		if !isSafeTokenFileName(hc.Token) {
			slog.Warn("Skipping cask with unsafe token for file path", "token", hc.Token)
			skipped++
			continue
		}

		urlMap := map[string]string{"darwin_arm64": hc.URL, "darwin_amd64": hc.URL}
		shaMap := map[string]string{"darwin_arm64": hc.SHA256, "darwin_amd64": hc.SHA256}
		sha512Map := map[string]string{"darwin_arm64": hc.SHA512, "darwin_amd64": hc.SHA512}

		for _, pm := range platforms {
			if !strings.HasPrefix(pm.key, "darwin") {
				continue
			}
			for _, pref := range pm.prefs {
				if v, ok := hc.Variations[pref]; ok {
					urlMap[pm.key] = v.URL
					shaMap[pm.key] = v.SHA256
					sha512Map[pm.key] = v.SHA512
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
			License: hc.License, URL: urlMap, SHA256: shaMap, SHA512: sha512Map, Artifacts: arts,
			Source: cask.SourceSpec{URL: hc.URL, SHA256: hc.SHA256, SHA512: hc.SHA512},
		}

		saveYAML(filepath.Join(outDir, hc.Token+".yaml"), c)
		imported++
	}
	slog.Info("Cask import complete", "imported", imported, "skipped", skipped)
}

func isSafeTokenFileName(token string) bool {
	if token == "" {
		return false
	}
	// Disallow path traversal and separators regardless of platform.
	if strings.Contains(token, "..") || strings.Contains(token, "/") || strings.Contains(token, "\\") {
		return false
	}
	// Allow common token characters only.
	ok, _ := regexp.MatchString(`^[a-z0-9][a-z0-9._+-]*$`, token)
	return ok
}

func parseCaskArtifacts(raw []json.RawMessage) cask.Artifacts {
	var res cask.Artifacts
	for _, m := range raw {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(m, &obj); err != nil {
			slog.Debug("artifact unmarshal failed", "error", err)
			continue
		}
		if app, ok := obj["app"]; ok {
			var apps []string
			if err := json.Unmarshal(app, &apps); err != nil {
				slog.Debug("app unmarshal failed", "error", err)
			} else {
				res.App = append(res.App, apps...)
			}
		}
		if bin, ok := obj["binary"]; ok {
			var binArr []json.RawMessage
			if err := json.Unmarshal(bin, &binArr); err != nil {
				slog.Debug("binary array unmarshal failed", "error", err)
			} else if len(binArr) > 0 {
				var path string
				if err := json.Unmarshal(binArr[0], &path); err != nil {
					slog.Debug("binary path unmarshal failed", "error", err)
				} else {
					name := filepath.Base(path)
					if len(binArr) > 1 {
						var opts map[string]string
						if err := json.Unmarshal(binArr[1], &opts); err != nil {
							slog.Debug("binary options unmarshal failed", "error", err)
						} else {
							if t, ok := opts["target"]; ok {
								name = t
							}
						}
					}
					res.Bin = append(res.Bin, name)
				}
			}
		}
	}
	return res
}

// --- Common Helpers ---

func safeJoinUnderBase(base, rel string) (string, error) {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	candidate := filepath.Clean(filepath.Join(baseAbs, rel))
	relativeToBase, err := filepath.Rel(baseAbs, candidate)
	if err != nil {
		return "", err
	}
	if relativeToBase == ".." || strings.HasPrefix(relativeToBase, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return candidate, nil
}

func fetchAPI(apiURL string) []byte {
	slog.Info("Fetching API", "url", apiURL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		slog.Error("Build request failed", "error", err)
		os.Exit(1)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("Fetch failed", "error", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Error("API error", "status", resp.Status)
		os.Exit(1)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		slog.Error("Read failed", "error", err)
		os.Exit(1)
	}
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
