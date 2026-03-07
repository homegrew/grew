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

const apiURL = "https://formulae.brew.sh/api/cask.json"

type hbCask struct {
	Token      string                     `json:"token"`
	Name       []string                   `json:"name"`
	Desc       string                     `json:"desc"`
	Homepage   string                     `json:"homepage"`
	URL        string                     `json:"url"`
	SHA256     string                     `json:"sha256"`
	Version    string                     `json:"version"`
	Artifacts  []json.RawMessage          `json:"artifacts"`
	Variations map[string]hbCaskVariation `json:"variations"`
	Deprecated bool                       `json:"deprecated"`
	Disabled   bool                       `json:"disabled"`
}

type hbCaskVariation struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type parsedArtifacts struct {
	App []string
	Bin []string
}

func parseArtifacts(raw []json.RawMessage) parsedArtifacts {
	var result parsedArtifacts
	for _, msg := range raw {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(msg, &obj); err != nil {
			continue
		}

		// Parse "app" artifacts: {"app": ["Name.app"]}
		if appRaw, ok := obj["app"]; ok {
			var apps []string
			if err := json.Unmarshal(appRaw, &apps); err == nil {
				for _, a := range apps {
					if strings.HasSuffix(a, ".app") {
						result.App = append(result.App, a)
					}
				}
			}
		}

		// Parse "binary" artifacts: {"binary": ["/path/to/bin"]} or {"binary": ["/path", {"target": "name"}]}
		if binRaw, ok := obj["binary"]; ok {
			var binArr []json.RawMessage
			if err := json.Unmarshal(binRaw, &binArr); err == nil && len(binArr) > 0 {
				// First element is the path string.
				var binPath string
				if err := json.Unmarshal(binArr[0], &binPath); err == nil {
					// Check for target override in second element.
					binName := filepath.Base(binPath)
					if len(binArr) > 1 {
						var opts map[string]string
						if err := json.Unmarshal(binArr[1], &opts); err == nil {
							if t, ok := opts["target"]; ok {
								binName = t
							}
						}
					}
					result.Bin = append(result.Bin, binName)
				}
			}
		}
	}
	return result
}

func yamlEscape(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, `:{}[]&*?|>!%@\'"#`) || strings.HasPrefix(s, " ") {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}

func urlExt(u string) string {
	lower := strings.ToLower(u)
	// Strip query string.
	if idx := strings.Index(lower, "?"); idx != -1 {
		lower = lower[:idx]
	}
	if strings.HasSuffix(lower, ".dmg") {
		return "dmg"
	}
	if strings.HasSuffix(lower, ".zip") {
		return "zip"
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return "tar.gz"
	}
	if strings.HasSuffix(lower, ".pkg") {
		return "pkg"
	}
	return ""
}

var darwinARM64Prefs = []string{
	"arm64_tahoe", "arm64_sequoia", "arm64_sonoma", "arm64_ventura", "arm64_monterey", "arm64_big_sur",
}
var darwinAMD64Prefs = []string{
	"tahoe", "sequoia", "sonoma", "ventura", "monterey", "big_sur", "catalina", "mojave",
}

type platformMapping struct {
	grewKey string
	prefs   []string
}

var macPlatforms = []platformMapping{
	{"darwin_arm64", darwinARM64Prefs},
	{"darwin_amd64", darwinAMD64Prefs},
}

func generateYAML(c *hbCask, urlMap, sha256Map map[string]string, arts parsedArtifacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", c.Token)
	fmt.Fprintf(&b, "version: %s\n", yamlEscape(c.Version))
	fmt.Fprintf(&b, "description: %s\n", yamlEscape(c.Desc))
	fmt.Fprintf(&b, "homepage: %s\n", c.Homepage)

	b.WriteString("url:\n")
	for _, pm := range macPlatforms {
		if u, ok := urlMap[pm.grewKey]; ok {
			fmt.Fprintf(&b, "  %s: %s\n", pm.grewKey, u)
		}
	}

	b.WriteString("sha256:\n")
	for _, pm := range macPlatforms {
		if s, ok := sha256Map[pm.grewKey]; ok {
			fmt.Fprintf(&b, "  %s: %s\n", pm.grewKey, s)
		}
	}

	if c.URL != "" {
		b.WriteString("source:\n")
		fmt.Fprintf(&b, "  url: %s\n", c.URL)
		fmt.Fprintf(&b, "  sha256: %s\n", c.SHA256)
	}

	b.WriteString("artifacts:\n")
	if len(arts.App) > 0 {
		b.WriteString("  app:\n")
		for _, app := range arts.App {
			fmt.Fprintf(&b, "    - %s\n", yamlEscape(app))
		}
	}
	if len(arts.Bin) > 0 {
		b.WriteString("  bin:\n")
		for _, bin := range arts.Bin {
			fmt.Fprintf(&b, "    - %s\n", yamlEscape(bin))
		}
	}

	return b.String()
}

func main() {
	outDir := "taps/cask"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	fmt.Fprintf(os.Stderr, "Fetching Homebrew cask index...\n")
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

	var casks []hbCask
	if err := json.Unmarshal(data, &casks); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Parsed %d casks from Homebrew API\n", len(casks))

	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error: create output dir: %v\n", err)
		os.Exit(1)
	}

	imported := 0
	skipped := 0
	skipReasons := map[string]int{}

	for i := range casks {
		c := &casks[i]

		if c.Deprecated || c.Disabled {
			skipped++
			skipReasons["deprecated/disabled"]++
			continue
		}

		if c.Version == "" || c.Version == "latest" {
			skipped++
			skipReasons["no stable version"]++
			continue
		}

		if c.URL == "" {
			skipped++
			skipReasons["no URL"]++
			continue
		}

		if !strings.HasPrefix(c.URL, "https://") {
			skipped++
			skipReasons["non-HTTPS URL"]++
			continue
		}

		if c.SHA256 == "" || c.SHA256 == "no_check" {
			skipped++
			skipReasons["no SHA256"]++
			continue
		}

		ext := urlExt(c.URL)
		if ext != "dmg" && ext != "zip" && ext != "tar.gz" {
			skipped++
			skipReasons["unsupported format ("+ext+")"]++
			continue
		}

		urlMap := map[string]string{}
		sha256Map := map[string]string{}

		// Base URL for both architectures by default
		urlMap["darwin_arm64"] = c.URL
		urlMap["darwin_amd64"] = c.URL
		sha256Map["darwin_arm64"] = c.SHA256
		sha256Map["darwin_amd64"] = c.SHA256

		// Overwrite with variations if found
		for _, pm := range macPlatforms {
			for _, pref := range pm.prefs {
				if v, ok := c.Variations[pref]; ok {
					urlMap[pm.grewKey] = v.URL
					sha256Map[pm.grewKey] = v.SHA256
					break // Use the most preferred (latest) OS version
				}
			}
		}

		arts := parseArtifacts(c.Artifacts)
		if len(arts.App) == 0 && len(arts.Bin) == 0 {
			skipped++
			skipReasons["no app/bin artifacts"]++
			continue
		}

		yaml := generateYAML(c, urlMap, sha256Map, arts)
		outPath := filepath.Join(outDir, c.Token+".yaml")
		if err := os.WriteFile(outPath, []byte(yaml), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write %s: %v\n", outPath, err)
			skipped++
			continue
		}

		imported++
	}

	fmt.Fprintf(os.Stderr, "\nDone: %d casks imported, %d skipped\n", imported, skipped)
	fmt.Fprintf(os.Stderr, "Output: %s/\n", outDir)

	// Print skip reasons sorted by count.
	type reason struct {
		name  string
		count int
	}
	var reasons []reason
	for k, v := range skipReasons {
		reasons = append(reasons, reason{k, v})
	}
	sort.Slice(reasons, func(i, j int) bool { return reasons[i].count > reasons[j].count })
	fmt.Fprintf(os.Stderr, "\nSkip reasons:\n")
	for _, r := range reasons {
		fmt.Fprintf(os.Stderr, "  %4d  %s\n", r.count, r.name)
	}
}
