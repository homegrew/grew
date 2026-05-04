package homebrew

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/formula"
)

var (
	formulaAPI = "https://formulae.brew.sh/api/formula/%s.json"
	caskAPI    = "https://formulae.brew.sh/api/cask/%s.json"
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
	KegOnly    bool `json:"keg_only"`
	Deprecated bool `json:"deprecated"`
	Disabled   bool `json:"disabled"`
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

func httpsGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "grew/1.0")
	return http.DefaultClient.Do(req)
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
				urlMap[pm.key] = v.URL
				shaMap[pm.key] = v.SHA256
				sha512Map[pm.key] = v.SHA512
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
		Caveats:     hc.Caveats,
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
			var apps []string
			if err := json.Unmarshal(app, &apps); err == nil {
				res.App = append(res.App, apps...)
			}
		}
		if pkg, ok := obj["pkg"]; ok {
			var pkgs []string
			if err := json.Unmarshal(pkg, &pkgs); err == nil {
				res.Pkg = append(res.Pkg, pkgs...)
			}
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
