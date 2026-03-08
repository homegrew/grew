package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/snapshot"
)

func setupTestKeg(t *testing.T, cel *cellar.Cellar, name, version string) string {
	t.Helper()
	kegDir := filepath.Join(cel.Path, name, version)
	if err := os.MkdirAll(filepath.Join(kegDir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	// Create a test binary.
	binPath := filepath.Join(kegDir, "bin", name)
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho hello\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return kegDir
}

func TestCheckManifestIntegrity_NoManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cel := &cellar.Cellar{Path: filepath.Join(dir, "Cellar")}
	kegPath := setupTestKeg(t, cel, "testpkg", "1.0.0")

	pkg := cellar.InstalledPackage{Name: "testpkg", Version: "1.0.0", Path: kegPath}
	findings := checkManifestIntegrity(pkg, kegPath)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != severityMedium {
		t.Errorf("expected medium severity, got %s", findings[0].Severity)
	}
	if findings[0].Category != "integrity" {
		t.Errorf("expected integrity category, got %s", findings[0].Category)
	}
}

func TestCheckManifestIntegrity_OK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cel := &cellar.Cellar{Path: filepath.Join(dir, "Cellar")}
	kegPath := setupTestKeg(t, cel, "goodpkg", "2.0.0")

	// Capture a manifest and then verify — should be OK.
	meta := snapshot.InstallMeta{Platform: "test_amd64"}
	m, err := snapshot.Capture("goodpkg", "2.0.0", kegPath, meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Save(m, kegPath); err != nil {
		t.Fatal(err)
	}

	pkg := cellar.InstalledPackage{Name: "goodpkg", Version: "2.0.0", Path: kegPath}
	findings := checkManifestIntegrity(pkg, kegPath)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckManifestIntegrity_ModifiedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cel := &cellar.Cellar{Path: filepath.Join(dir, "Cellar")}
	kegPath := setupTestKeg(t, cel, "modpkg", "1.0.0")

	meta := snapshot.InstallMeta{Platform: "test_amd64"}
	m, err := snapshot.Capture("modpkg", "1.0.0", kegPath, meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Save(m, kegPath); err != nil {
		t.Fatal(err)
	}

	// Modify a file after capture.
	os.WriteFile(filepath.Join(kegPath, "bin", "modpkg"), []byte("#!/bin/sh\necho TAMPERED\n"), 0755)

	pkg := cellar.InstalledPackage{Name: "modpkg", Version: "1.0.0", Path: kegPath}
	findings := checkManifestIntegrity(pkg, kegPath)

	if len(findings) == 0 {
		t.Fatal("expected findings for modified file, got none")
	}
	foundCritical := false
	for _, f := range findings {
		if f.Severity == severityCritical && f.Category == "integrity" {
			foundCritical = true
		}
	}
	if !foundCritical {
		t.Error("expected critical severity finding for modified file")
	}
}

func TestCheckKegPermissions_WorldWritable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cel := &cellar.Cellar{Path: filepath.Join(dir, "Cellar")}
	kegPath := setupTestKeg(t, cel, "wwpkg", "1.0.0")

	// Make the binary world-writable.
	os.Chmod(filepath.Join(kegPath, "bin", "wwpkg"), 0777)

	pkg := cellar.InstalledPackage{Name: "wwpkg", Version: "1.0.0", Path: kegPath}
	findings := checkKegPermissions(pkg, kegPath)

	if len(findings) == 0 {
		t.Fatal("expected finding for world-writable file, got none")
	}
	if findings[0].Category != "permissions" {
		t.Errorf("expected permissions category, got %s", findings[0].Category)
	}
}

func TestCheckKegPermissions_Clean(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cel := &cellar.Cellar{Path: filepath.Join(dir, "Cellar")}
	kegPath := setupTestKeg(t, cel, "cleanpkg", "1.0.0")

	pkg := cellar.InstalledPackage{Name: "cleanpkg", Version: "1.0.0", Path: kegPath}
	findings := checkKegPermissions(pkg, kegPath)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %+v", len(findings), findings)
	}
}

func TestCheckOutdatedVersion(t *testing.T) {
	t.Parallel()
	formulaMap := map[string]*formula.Formula{
		"oldpkg": {Name: "oldpkg", Version: "2.0.0"},
	}

	pkg := cellar.InstalledPackage{Name: "oldpkg", Version: "1.0.0"}
	findings := checkOutdatedVersion(pkg, formulaMap)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != severityLow {
		t.Errorf("expected low severity, got %s", findings[0].Severity)
	}
	if findings[0].Category != "outdated" {
		t.Errorf("expected outdated category, got %s", findings[0].Category)
	}
}

func TestCheckOutdatedVersion_Current(t *testing.T) {
	t.Parallel()
	formulaMap := map[string]*formula.Formula{
		"curpkg": {Name: "curpkg", Version: "1.0.0"},
	}

	pkg := cellar.InstalledPackage{Name: "curpkg", Version: "1.0.0"}
	findings := checkOutdatedVersion(pkg, formulaMap)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestCheckFormulaSecurity_HTTPUrl(t *testing.T) {
	t.Parallel()
	formulaMap := map[string]*formula.Formula{
		"httppkg": {
			Name:    "httppkg",
			Version: "1.0.0",
			URL:     map[string]string{"darwin_arm64": "http://example.com/pkg.tar.gz"},
		},
	}

	pkg := cellar.InstalledPackage{Name: "httppkg", Version: "1.0.0"}
	findings := checkFormulaSecurity(pkg, formulaMap)

	foundTransport := false
	for _, f := range findings {
		if f.Category == "transport" && f.Severity == severityCritical {
			foundTransport = true
		}
	}
	if !foundTransport {
		t.Error("expected critical transport finding for HTTP URL")
	}
}

func TestCheckFormulaSecurity_HTTPS(t *testing.T) {
	t.Parallel()
	formulaMap := map[string]*formula.Formula{
		"securepkg": {
			Name:    "securepkg",
			Version: "1.0.0",
			URL:     map[string]string{"darwin_arm64": "https://example.com/pkg.tar.gz"},
			SHA256:  map[string]string{"darwin_arm64": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		},
	}

	pkg := cellar.InstalledPackage{Name: "securepkg", Version: "1.0.0"}
	findings := checkFormulaSecurity(pkg, formulaMap)

	if len(findings) != 0 {
		t.Errorf("expected 0 findings for secure formula, got %d: %+v", len(findings), findings)
	}
}

func TestScanGlobalPermissions_WorldWritableDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths := config.Paths{
		Root:   dir,
		Cellar: filepath.Join(dir, "Cellar"),
		Bin:    filepath.Join(dir, "bin"),
		Lib:    filepath.Join(dir, "lib"),
		Opt:    filepath.Join(dir, "opt"),
		Taps:   filepath.Join(dir, "Taps"),
	}

	// Create and make one dir world-writable.
	os.MkdirAll(paths.Bin, 0755)
	os.Chmod(paths.Bin, 0777)

	findings := scanGlobalPermissions(paths)

	foundPerm := false
	for _, f := range findings {
		if f.Category == "permissions" && f.Severity == severityCritical {
			foundPerm = true
		}
	}
	if !foundPerm {
		t.Error("expected critical permissions finding for world-writable directory")
	}
}

func TestVulnSummary(t *testing.T) {
	t.Parallel()
	findings := []vulnFinding{
		{Severity: severityCritical},
		{Severity: severityCritical},
		{Severity: severityHigh},
		{Severity: severityMedium},
		{Severity: severityLow},
		{Severity: severityLow},
	}
	summary := vulnSummary(findings)
	if summary["critical"] != 2 {
		t.Errorf("expected 2 critical, got %d", summary["critical"])
	}
	if summary["high"] != 1 {
		t.Errorf("expected 1 high, got %d", summary["high"])
	}
	if summary["medium"] != 1 {
		t.Errorf("expected 1 medium, got %d", summary["medium"])
	}
	if summary["low"] != 2 {
		t.Errorf("expected 2 low, got %d", summary["low"])
	}
}

// --- Tests for OSV integration helpers ---

func TestExtractRepoURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		f    *formula.Formula
		want string
	}{
		{
			name: "github source URL",
			f: &formula.Formula{
				Source: formula.SourceSpec{URL: "https://github.com/jqlang/jq/archive/refs/tags/jq-1.7.1.tar.gz"},
			},
			want: "https://github.com/jqlang/jq",
		},
		{
			name: "github releases URL",
			f: &formula.Formula{
				URL: map[string]string{
					"darwin_arm64": "https://github.com/BurntSushi/ripgrep/releases/download/14.1.0/ripgrep-14.1.0-aarch64-apple-darwin.tar.gz",
				},
			},
			want: "https://github.com/BurntSushi/ripgrep",
		},
		{
			name: "github homepage",
			f: &formula.Formula{
				Homepage: "https://github.com/stedolan/jq",
			},
			want: "https://github.com/stedolan/jq",
		},
		{
			name: "gitlab URL",
			f: &formula.Formula{
				Source: formula.SourceSpec{URL: "https://gitlab.com/gnuwget/wget2/archive/v2.1.0.tar.gz"},
			},
			want: "https://gitlab.com/gnuwget/wget2",
		},
		{
			name: "codeberg URL",
			f: &formula.Formula{
				Homepage: "https://codeberg.org/dnkl/foot",
			},
			want: "https://codeberg.org/dnkl/foot",
		},
		{
			name: "no repo URL",
			f: &formula.Formula{
				Homepage: "https://www.openssl.org/",
				URL:      map[string]string{"darwin_arm64": "https://www.openssl.org/source/openssl-3.2.0.tar.gz"},
			},
			want: "",
		},
		{
			name: "source preferred over homepage",
			f: &formula.Formula{
				Source:   formula.SourceSpec{URL: "https://github.com/correct/repo/archive/v1.0.tar.gz"},
				Homepage: "https://github.com/old/repo",
			},
			want: "https://github.com/correct/repo",
		},
		{
			name: ".git suffix stripped",
			f: &formula.Formula{
				Homepage: "https://github.com/user/repo.git",
			},
			want: "https://github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractRepoURL(tt.f)
			if got != tt.want {
				t.Errorf("extractRepoURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractTagFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/jqlang/jq/archive/refs/tags/jq-1.7.1.tar.gz", "jq-1.7.1"},
		{"https://github.com/jqlang/jq/archive/refs/tags/v1.7.1.tar.gz", "v1.7.1"},
		{"https://github.com/BurntSushi/ripgrep/releases/download/14.1.0/ripgrep-14.1.0.tar.gz", "14.1.0"},
		{"https://github.com/foo/bar/archive/v2.0.0.zip", "v2.0.0"},
		{"https://codeberg.org/dnkl/foot/archive/1.16.2.tar.gz", "1.16.2"},
		{"https://www.openssl.org/source/openssl-3.2.0.tar.gz", ""},
	}

	for _, tt := range tests {
		got := extractTagFromURL(tt.url)
		if got != tt.want {
			t.Errorf("extractTagFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestExtractVersionTag(t *testing.T) {
	t.Parallel()

	// Formula with source URL containing a tag.
	f := &formula.Formula{
		Source: formula.SourceSpec{URL: "https://github.com/jqlang/jq/archive/refs/tags/jq-1.7.1.tar.gz"},
	}
	got := extractVersionTag(f, "1.7.1")
	if got != "jq-1.7.1" {
		t.Errorf("expected jq-1.7.1, got %s", got)
	}

	// Formula with no parseable tag — should fallback to installed version.
	f2 := &formula.Formula{
		URL: map[string]string{"darwin_arm64": "https://www.openssl.org/source/openssl-3.2.0.tar.gz"},
	}
	got2 := extractVersionTag(f2, "3.2.0")
	if got2 != "3.2.0" {
		t.Errorf("expected 3.2.0 fallback, got %s", got2)
	}
}

func TestMapOSVSeverity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  vulnSeverity
	}{
		{"critical", severityCritical},
		{"high", severityHigh},
		{"medium", severityMedium},
		{"low", severityLow},
		{"unknown", severityMedium},
		{"", severityMedium},
	}

	for _, tt := range tests {
		got := mapOSVSeverity(tt.input)
		if got != tt.want {
			t.Errorf("mapOSVSeverity(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
