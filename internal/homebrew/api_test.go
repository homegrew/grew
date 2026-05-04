package homebrew

import (
	"net/http"
	"net/http/httptest"
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
