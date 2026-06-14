package homebrew

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/homegrew/grew/pkg/cask"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/validation"
)

var (
	formulaAPI     = "https://formulae.brew.sh/api/formula/%s.json"
	caskAPI        = "https://formulae.brew.sh/api/cask/%s.json"
	formulaListAPI = "https://formulae.brew.sh/api/formula.json"
	caskListAPI    = "https://formulae.brew.sh/api/cask.json"
)

type hbFormula struct {
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Homepage string `json:"homepage"`
	License  string `json:"license"`
	Caveats  string `json:"caveats"`
	Versions struct {
		Stable string `json:"stable"`
		Head   string `json:"head"`
	} `json:"versions"`
	Urls struct {
		Stable struct {
			URL      string `json:"url"`
			Checksum string `json:"checksum"`
		} `json:"stable"`
		Head struct {
			URL    string `json:"url"`
			Branch string `json:"branch"`
			Using  string `json:"using"`
		} `json:"head"`
	} `json:"urls"`
	Bottle struct {
		Stable struct {
			Files map[string]struct {
				URL    string `json:"url"`
				SHA256 string `json:"sha256"`
				Cellar string `json:"cellar"`
			} `json:"files"`
		} `json:"stable"`
	} `json:"bottle"`
	Dependencies      []string `json:"dependencies"`
	BuildDependencies []string `json:"build_dependencies"`
	Variations        map[string]struct {
		URL               string   `json:"url"`
		Checksum          string   `json:"checksum"`
		Dependencies      []string `json:"dependencies"`
		BuildDependencies []string `json:"build_dependencies"`
	} `json:"variations"`
	Service    *hbService `json:"service"`
	KegOnly    bool       `json:"keg_only"`
	Deprecated bool       `json:"deprecated"`
	Disabled   bool       `json:"disabled"`
}

type hbService struct {
	Run        any    `json:"run"`
	RunType    string `json:"run_type"`
	KeepAlive  any    `json:"keep_alive"`
	WorkingDir string `json:"working_dir"`
	LogPath    string `json:"log_path"`
	ErrorLog   string `json:"error_log_path"`
}

type hbCask struct {
	Token      string            `json:"token"`
	Name       []string          `json:"name"`
	Desc       string            `json:"desc"`
	Homepage   string            `json:"homepage"`
	License    string            `json:"license"`
	Caveats    string            `json:"caveats"`
	URL        string            `json:"url"`
	SHA256     string            `json:"sha256"`
	SHA512     string            `json:"sha512"`
	Version    string            `json:"version"`
	Artifacts  []json.RawMessage `json:"artifacts"`
	Variations map[string]struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
		SHA512 string `json:"sha512"`
	} `json:"variations"`
	Deprecated bool `json:"deprecated"`
	Disabled   bool `json:"disabled"`
}

var platforms = []struct {
	key   string
	prefs []string
}{
	{"darwin_arm64_15", []string{"arm64_sequoia", "arm64_sonoma", "arm64_ventura", "all"}},
	{"darwin_arm64_14", []string{"arm64_sonoma", "arm64_ventura", "arm64_monterey", "all"}},
	{"darwin_arm64_13", []string{"arm64_ventura", "arm64_monterey", "arm64_big_sur", "all"}},
	{"darwin_arm64_12", []string{"arm64_monterey", "arm64_big_sur", "all"}},
	{"darwin_arm64_11", []string{"arm64_big_sur", "all"}},
	{"darwin_arm64", []string{"arm64_sequoia", "arm64_sonoma", "arm64_ventura", "arm64_monterey", "arm64_big_sur", "all"}},
	{"darwin_amd64_15", []string{"sequoia", "sonoma", "ventura", "all"}},
	{"darwin_amd64_14", []string{"sonoma", "ventura", "monterey", "all"}},
	{"darwin_amd64_13", []string{"ventura", "monterey", "big_sur", "all"}},
	{"darwin_amd64_12", []string{"monterey", "big_sur", "all"}},
	{"darwin_amd64_11", []string{"big_sur", "all"}},
	{"darwin_amd64_10", []string{"catalina", "mojave", "high_sierra", "sierra", "all"}},
	{"darwin_amd64", []string{"sequoia", "sonoma", "ventura", "monterey", "big_sur", "catalina", "mojave", "all"}},
	{"linux_amd64", []string{"x86_64_linux", "all"}},
	{"linux_arm64", []string{"arm64_linux", "all"}},
}

func FetchFormula(name string) (*formula.Formula, error) {
	if !validation.IsValidName(name) {
		return nil, fmt.Errorf("invalid formula name: %q", name)
	}

	url := fmt.Sprintf(formulaAPI, name)
	resp, err := httpsGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("formula %q not found on Homebrew API", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Homebrew API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var hf hbFormula
	if err := json.Unmarshal(data, &hf); err != nil {
		return nil, err
	}

	return convertFormula(&hf), nil
}

func FetchCask(token string) (*cask.Cask, error) {
	if !validation.IsValidName(token) {
		return nil, fmt.Errorf("invalid cask token: %q", token)
	}

	url := fmt.Sprintf(caskAPI, token)
	resp, err := httpsGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("cask %q not found on Homebrew API", token)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Homebrew API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var hc hbCask
	if err := json.Unmarshal(data, &hc); err != nil {
		return nil, err
	}

	return convertCask(&hc), nil
}

func FetchAllFormulae() ([]*formula.Formula, error) {
	resp, err := httpsGet(formulaListAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Homebrew API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20)) // Allow up to 256MB
	if err != nil {
		return nil, err
	}

	var hfs []hbFormula
	if err := json.Unmarshal(data, &hfs); err != nil {
		return nil, err
	}

	var result []*formula.Formula
	for i := range hfs {
		// Genre-specific skipping logic can be applied here or in the caller
		hf := &hfs[i]
		if hf.Deprecated || hf.Disabled || (hf.Versions.Stable == "" && hf.Versions.Head == "") {
			continue
		}
		result = append(result, convertFormula(hf))
	}

	return result, nil
}

func FetchAllCasks() ([]*cask.Cask, error) {
	resp, err := httpsGet(caskListAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Homebrew API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20)) // Allow up to 256MB
	if err != nil {
		return nil, err
	}

	var hcs []hbCask
	if err := json.Unmarshal(data, &hcs); err != nil {
		return nil, err
	}

	var result []*cask.Cask
	for i := range hcs {
		hc := &hcs[i]
		if hc.Deprecated || hc.Disabled || hc.Version == "" || hc.Version == "latest" || (hc.SHA256 == "" && hc.SHA512 == "") || hc.SHA256 == "no_check" {
			continue
		}
		result = append(result, convertCask(hc))
	}

	return result, nil
}

// FetchFormulaNames returns the name of every non-deprecated, non-disabled formula.
func FetchFormulaNames() ([]string, error) {
	resp, err := httpsGet(formulaListAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Homebrew API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}

	var items []struct {
		Name       string `json:"name"`
		Deprecated bool   `json:"deprecated"`
		Disabled   bool   `json:"disabled"`
		Versions   struct {
			Stable string `json:"stable"`
			Head   string `json:"head"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.Deprecated || item.Disabled || (item.Versions.Stable == "" && item.Versions.Head == "") {
			continue
		}
		names = append(names, item.Name)
	}
	return names, nil
}

// FetchCaskNames returns the token of every non-deprecated, non-disabled cask.
func FetchCaskNames() ([]string, error) {
	resp, err := httpsGet(caskListAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Homebrew API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return nil, err
	}

	var items []struct {
		Token      string `json:"token"`
		Deprecated bool   `json:"deprecated"`
		Disabled   bool   `json:"disabled"`
		Version    string `json:"version"`
		SHA256     string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.Deprecated || item.Disabled || item.Version == "" || item.Version == "latest" || item.SHA256 == "" || item.SHA256 == "no_check" {
			continue
		}
		names = append(names, item.Token)
	}
	return names, nil
}

var apiClient = &http.Client{Timeout: 60 * time.Second} // Increased timeout for bulk fetches

func httpsGet(rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	host := u.Hostname()
	isLocal := host == "localhost" || host == "127.0.0.1"

	if u.Scheme != "https" && !isLocal {
		return nil, fmt.Errorf("refusing non-HTTPS URL: %s", rawURL)
	}

	// Strictly validate the hostname to prevent SSRF vulnerabilities.
	if host != "formulae.brew.sh" && !isLocal {
		return nil, fmt.Errorf("host %q is not permitted for Homebrew API requests", host)
	}

	// Reconstruct the URL from validated components.
	safe := &url.URL{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Path:     u.Path,
		RawQuery: u.RawQuery,
	}

	req, err := http.NewRequest("GET", safe.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "grew/1.0")
	return apiClient.Do(req)
}

func convertFormula(hf *hbFormula) *formula.Formula {
	bottleMap := map[string]formula.BottleSpec{}
	for _, pm := range platforms {
		for _, pref := range pm.prefs {
			if f, ok := hf.Bottle.Stable.Files[pref]; ok {
				bottleMap[pm.key] = formula.BottleSpec{
					URL:    f.URL,
					SHA256: f.SHA256,
					Cellar: f.Cellar,
				}
				break
			}
		}
	}

	urlMap := make(map[string]string)
	shaMap := make(map[string]string)
	for _, pm := range platforms {
		urlMap[pm.key] = hf.Urls.Stable.URL
		shaMap[pm.key] = hf.Urls.Stable.Checksum

		for _, pref := range pm.prefs {
			if v, ok := hf.Variations[pref]; ok {
				if v.URL != "" {
					urlMap[pm.key] = v.URL
				}
				if v.Checksum != "" {
					shaMap[pm.key] = v.Checksum
				}
				break
			}
		}
	}

	version := hf.Versions.Stable
	if version == "" {
		version = hf.Versions.Head
	}

	f := &formula.Formula{
		Name:              hf.Name,
		Version:           version,
		Description:       hf.Desc,
		Homepage:          hf.Homepage,
		License:           hf.License,
		Caveats:           hf.Caveats,
		URL:               urlMap,
		SHA256:            shaMap,
		Bottle:            bottleMap,
		Dependencies:      hf.Dependencies,
		BuildDependencies: hf.BuildDependencies,
		KegOnly:           hf.KegOnly,
		Tap:               "homebrew/core (remote)",
		Install: formula.InstallSpec{
			Type:            "archive",
			Format:          "tar.gz",
			StripComponents: 2,
		},
	}

	if hf.Urls.Stable.URL != "" {
		f.Source = &formula.SourceSpec{
			URL:    hf.Urls.Stable.URL,
			SHA256: hf.Urls.Stable.Checksum,
		}
	}

	if hf.Urls.Head.URL != "" {
		f.Head = &formula.HeadSpec{
			URL:    hf.Urls.Head.URL,
			Branch: hf.Urls.Head.Branch,
			Using:  hf.Urls.Head.Using,
		}
	}

	if hf.Service != nil && hf.Service.Run != nil {
		var runCmd []string
		switch v := hf.Service.Run.(type) {
		case string:
			runCmd = []string{v}
		case []any:
			for _, arg := range v {
				if strArg, ok := arg.(string); ok {
					runCmd = append(runCmd, strArg)
				}
			}
		}

		if len(runCmd) > 0 {
			f.Service = &formula.ServiceSpec{
				Run:          runCmd,
				RunType:      hf.Service.RunType,
				WorkingDir:   hf.Service.WorkingDir,
				LogPath:      hf.Service.LogPath,
				ErrorLogPath: hf.Service.ErrorLog,
			}
			if hf.Service.KeepAlive != nil {
				switch v := hf.Service.KeepAlive.(type) {
				case bool:
					f.Service.KeepAlive = v
				default:
					f.Service.KeepAlive = true
				}
			}
		}
	}

	return f
}

func convertCask(hc *hbCask) *cask.Cask {
	urlMap := make(map[string]string)
	shaMap := make(map[string]string)
	sha512Map := make(map[string]string)

	for _, pm := range platforms {
		if !strings.HasPrefix(pm.key, "darwin") {
			continue
		}
		urlMap[pm.key] = hc.URL
		shaMap[pm.key] = hc.SHA256
		sha512Map[pm.key] = hc.SHA512

		for _, pref := range pm.prefs {
			if v, ok := hc.Variations[pref]; ok {
				if v.URL != "" {
					urlMap[pm.key] = v.URL
				}
				if v.SHA256 != "" {
					shaMap[pm.key] = v.SHA256
				}
				if v.SHA512 != "" {
					sha512Map[pm.key] = v.SHA512
				}
				break
			}
		}
	}

	arts := parseCaskArtifacts(hc.Artifacts)

	return &cask.Cask{
		Name:        hc.Token,
		Version:     hc.Version,
		Description: hc.Desc,
		Homepage:    hc.Homepage,
		License:     hc.License,
		Caveats:     rewriteHomebrewPrefix(hc.Caveats),
		URL:         urlMap,
		SHA256:      shaMap,
		SHA512:      sha512Map,
		Artifacts:   arts,
		Source:      cask.SourceSpec{URL: hc.URL, SHA256: hc.SHA256, SHA512: hc.SHA512},
		Tap:         "homebrew/cask (remote)",
	}
}

func parseCaskArtifacts(raw []json.RawMessage) cask.Artifacts {
	var res cask.Artifacts
	for _, m := range raw {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(m, &obj); err != nil {
			continue
		}
		if app, ok := obj["app"]; ok {
			// An app artifact is an array whose elements are either the
			// ".app" filename (string) or an option object such as
			// {"target": "Renamed.app"}. The target, when present, renames
			// the app in /Applications and is the name that actually lands
			// on disk. Unmarshal element-by-element so a trailing options
			// object doesn't abort parsing of the whole array.
			var appArr []json.RawMessage
			if err := json.Unmarshal(app, &appArr); err == nil {
				name := ""
				for _, el := range appArr {
					var s string
					if err := json.Unmarshal(el, &s); err == nil {
						if name != "" {
							res.App = append(res.App, name)
						}
						name = s
						continue
					}
					var opts map[string]string
					if err := json.Unmarshal(el, &opts); err == nil {
						if t, ok := opts["target"]; ok {
							name = t
						}
					}
				}
				if name != "" {
					res.App = append(res.App, name)
				}
			}
		}
		if pkg, ok := obj["pkg"]; ok {
			// A pkg artifact is an array whose first element is the .pkg
			// filename and whose remaining elements (if any) are option
			// objects, e.g. ["VirtualBox.pkg", {"choices": [...]}]. Unmarshal
			// element-by-element so the trailing objects don't abort parsing.
			var pkgArr []json.RawMessage
			if err := json.Unmarshal(pkg, &pkgArr); err == nil {
				for _, el := range pkgArr {
					var name string
					if err := json.Unmarshal(el, &name); err == nil {
						res.Pkg = append(res.Pkg, name)
					}
				}
			}
		}
		if font, ok := obj["font"]; ok {
			var fonts []string
			if err := json.Unmarshal(font, &fonts); err == nil {
				res.Font = append(res.Font, fonts...)
			}
		}
		if installer, ok := obj["installer"]; ok {
			res.Installer = append(res.Installer, parseInstallerArtifact(installer)...)
		}
		if bin, ok := obj["binary"]; ok {
			var s string
			if err := json.Unmarshal(bin, &s); err == nil {
				res.Bin = append(res.Bin, filepath.Base(s))
			} else {
				var binArr []json.RawMessage
				if err := json.Unmarshal(bin, &binArr); err == nil && len(binArr) > 0 {
					var path string
					if err := json.Unmarshal(binArr[0], &path); err == nil {
						name := filepath.Base(path)
						if len(binArr) > 1 {
							var opts map[string]string
							if err := json.Unmarshal(binArr[1], &opts); err == nil {
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
	}
	return res
}

// parseInstallerArtifact parses a Homebrew "installer" artifact, extracting the
// "script" form (executable + args + sudo). The "manual" form is ignored: it
// has no executable for grew to run. $HOMEBREW_PREFIX references in the args are
// rewritten to $HOMEGREW_PREFIX, which grew expands at install time.
func parseInstallerArtifact(raw json.RawMessage) []cask.InstallerScript {
	// The installer artifact may be a single object or an array of objects,
	// each holding a "script" (or "manual") key.
	var objs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objs); err != nil {
		var single map[string]json.RawMessage
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil
		}
		objs = []map[string]json.RawMessage{single}
	}

	var scripts []cask.InstallerScript
	for _, obj := range objs {
		scriptRaw, ok := obj["script"]
		if !ok {
			continue
		}
		var s struct {
			Executable string   `json:"executable"`
			Args       []string `json:"args"`
			Sudo       bool     `json:"sudo"`
		}
		if err := json.Unmarshal(scriptRaw, &s); err != nil || s.Executable == "" {
			continue
		}
		args := make([]string, len(s.Args))
		for i, a := range s.Args {
			args[i] = rewriteHomebrewPrefix(a)
		}
		scripts = append(scripts, cask.InstallerScript{
			Executable: s.Executable,
			Args:       args,
			Sudo:       s.Sudo,
		})
	}
	return scripts
}

// rewriteHomebrewPrefix replaces Homebrew's $HOMEBREW_PREFIX placeholder with
// grew's $HOMEGREW_PREFIX in converted cask data.
func rewriteHomebrewPrefix(s string) string {
	return strings.NewReplacer(
		"${HOMEBREW_PREFIX}", "${HOMEGREW_PREFIX}",
		"$HOMEBREW_PREFIX", "$HOMEGREW_PREFIX",
	).Replace(s)
}
