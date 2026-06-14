package homebrew

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestFetchFormula(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/formula/wget.json" {
			t.Errorf("expected path /api/formula/wget.json, got %s", r.URL.Path)
		}
		json := `{
			"name": "wget",
			"desc": "Internet file retriever",
			"homepage": "https://www.gnu.org/software/wget/",
			"versions": { "stable": "1.21.1" },
			"urls": {
				"stable": {
					"url": "https://ftp.gnu.org/gnu/wget/wget-1.21.1.tar.gz",
					"checksum": "sha256-abc"
				}
			},
			"bottle": {
				"stable": {
					"files": {
						"arm64_sequoia": {
							"url": "https://example.com/wget-1.21.1.arm64_sequoia.bottle.tar.gz",
							"sha256": "sha256-def"
						}
					}
				}
			}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(json))
	}))
	defer server.Close()

	// Override API base for testing
	origFormulaAPI := formulaAPI
	formulaAPI = server.URL + "/api/formula/%s.json"
	defer func() { formulaAPI = origFormulaAPI }()

	f, err := FetchFormula("wget")
	if err != nil {
		t.Fatalf("FetchFormula failed: %v", err)
	}

	if f.Name != "wget" {
		t.Errorf("expected name wget, got %s", f.Name)
	}
	if f.Version != "1.21.1" {
		t.Errorf("expected version 1.21.1, got %s", f.Version)
	}
	if f.Description != "Internet file retriever" {
		t.Errorf("expected description, got %s", f.Description)
	}
}

func TestFetchCask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cask/firefox.json" {
			t.Errorf("expected path /api/cask/firefox.json, got %s", r.URL.Path)
		}
		json := `{
			"token": "firefox",
			"desc": "Web browser",
			"homepage": "https://www.mozilla.org/firefox/",
			"url": "https://download.mozilla.org/?product=firefox-latest",
			"sha256": "sha256-123",
			"version": "89.0",
			"artifacts": [
				{ "app": ["Firefox.app"] }
			]
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(json))
	}))
	defer server.Close()

	// Override API base for testing
	origCaskAPI := caskAPI
	caskAPI = server.URL + "/api/cask/%s.json"
	defer func() { caskAPI = origCaskAPI }()

	c, err := FetchCask("firefox")
	if err != nil {
		t.Fatalf("FetchCask failed: %v", err)
	}

	if c.Name != "firefox" {
		t.Errorf("expected name firefox, got %s", c.Name)
	}
	if c.Version != "89.0" {
		t.Errorf("expected version 89.0, got %s", c.Version)
	}
	if len(c.Artifacts.App) != 1 || c.Artifacts.App[0] != "Firefox.app" {
		t.Errorf("expected 1 app artifact Firefox.app, got %v", c.Artifacts.App)
	}
}

func TestParseCaskArtifacts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		wantApp  []string
		wantPkg  []string
		wantBin  []string
		wantFont []string
	}{
		{
			name:    "app",
			raw:     `[{"app": ["Firefox.app"]}]`,
			wantApp: []string{"Firefox.app"},
		},
		{
			name:    "pkg plain",
			raw:     `[{"pkg": ["Foo.pkg"]}]`,
			wantPkg: []string{"Foo.pkg"},
		},
		{
			name:    "pkg with options",
			raw:     `[{"pkg": ["VirtualBox.pkg", {"choices": [{"choiceIdentifier": "x"}]}]}]`,
			wantPkg: []string{"VirtualBox.pkg"},
		},
		{
			name:    "binary array with target",
			raw:     `[{"binary": ["bin/foo", {"target": "foo"}]}]`,
			wantBin: []string{"foo"},
		},
		{
			name:     "font",
			raw:      `[{"font": ["fonts/MyFont.otf"]}, {"font": ["fonts/Other.ttf"]}]`,
			wantFont: []string{"fonts/MyFont.otf", "fonts/Other.ttf"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var raw []json.RawMessage
			if err := json.Unmarshal([]byte(tt.raw), &raw); err != nil {
				t.Fatalf("unmarshal test input: %v", err)
			}
			got := parseCaskArtifacts(raw)
			if !reflect.DeepEqual(got.App, tt.wantApp) {
				t.Errorf("App = %v, want %v", got.App, tt.wantApp)
			}
			if !reflect.DeepEqual(got.Pkg, tt.wantPkg) {
				t.Errorf("Pkg = %v, want %v", got.Pkg, tt.wantPkg)
			}
			if !reflect.DeepEqual(got.Bin, tt.wantBin) {
				t.Errorf("Bin = %v, want %v", got.Bin, tt.wantBin)
			}
			if !reflect.DeepEqual(got.Font, tt.wantFont) {
				t.Errorf("Font = %v, want %v", got.Font, tt.wantFont)
			}
		})
	}
}

func TestParseInstallerArtifact(t *testing.T) {
	t.Parallel()

	// Script form with sudo and a $HOMEBREW_PREFIX arg (as anaconda ships it).
	raw := []byte(`[{"script": {"executable": "Install.sh", "args": ["-b", "-p", "$HOMEBREW_PREFIX/anaconda3"], "sudo": true}}]`)
	got := parseInstallerArtifact(raw)
	if len(got) != 1 {
		t.Fatalf("expected 1 installer script, got %d", len(got))
	}
	if got[0].Executable != "Install.sh" || !got[0].Sudo {
		t.Errorf("unexpected script: %+v", got[0])
	}
	wantArgs := []string{"-b", "-p", "$HOMEGREW_PREFIX/anaconda3"}
	if !reflect.DeepEqual(got[0].Args, wantArgs) {
		t.Errorf("args = %v, want %v (HOMEBREW_PREFIX should be rewritten)", got[0].Args, wantArgs)
	}

	// Single-object (non-array) form is also accepted.
	single := []byte(`{"script": {"executable": "run.sh"}}`)
	if g := parseInstallerArtifact(single); len(g) != 1 || g[0].Executable != "run.sh" {
		t.Errorf("single-object form not parsed: %+v", g)
	}

	// Manual form has no executable and is ignored.
	manual := []byte(`[{"manual": "Installer.app"}]`)
	if g := parseInstallerArtifact(manual); len(g) != 0 {
		t.Errorf("manual installer should be ignored, got %+v", g)
	}
}

func TestRewriteHomebrewPrefix(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"$HOMEBREW_PREFIX/bin":   "$HOMEGREW_PREFIX/bin",
		"${HOMEBREW_PREFIX}/lib": "${HOMEGREW_PREFIX}/lib",
		"no placeholder here":    "no placeholder here",
	}
	for in, want := range cases {
		if got := rewriteHomebrewPrefix(in); got != want {
			t.Errorf("rewriteHomebrewPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
