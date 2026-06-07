package formula

import (
	"runtime"
	"strings"
	"testing"

	"github.com/homegrew/grew/pkg/validation"
)

// validSHA is a valid 64-char hex SHA256 for tests.
const validSHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// validSHA512 is a valid 128-char hex SHA512 for tests (the SHA512 of empty input).
const validSHA512 = "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"

const validYAML = `
name: testpkg
version: "1.0.0"
description: "A test package"
homepage: "https://example.com"
license: "MIT"
url:
  darwin_arm64: "https://example.com/testpkg-darwin-arm64"
  linux_amd64: "https://example.com/testpkg-linux-amd64"
sha256:
  darwin_arm64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  linux_amd64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
install:
  type: binary
  binary_name: testpkg
dependencies:
  - dep1
  - dep2
keg_only: false
`

func TestParse_ValidYAML(t *testing.T) {
	f, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != "testpkg" {
		t.Errorf("name = %q, want %q", f.Name, "testpkg")
	}
	if f.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", f.Version, "1.0.0")
	}
	if f.Description != "A test package" {
		t.Errorf("description = %q, want %q", f.Description, "A test package")
	}
	if f.Install.Type != "binary" {
		t.Errorf("install.type = %q, want %q", f.Install.Type, "binary")
	}
	if f.Install.BinaryName != "testpkg" {
		t.Errorf("install.binary_name = %q, want %q", f.Install.BinaryName, "testpkg")
	}
	if len(f.Dependencies) != 2 {
		t.Errorf("dependencies len = %d, want 2", len(f.Dependencies))
	}
	if len(f.URL) != 2 {
		t.Errorf("url map len = %d, want 2", len(f.URL))
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte(`{{{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParse_MissingName(t *testing.T) {
	yml := `
version: "1.0"
url:
  linux_amd64: "https://example.com/internal"
install:
  type: binary
`
	_, err := Parse([]byte(yml))
	if err == nil {
		t.Fatal("expected validation error for missing name")
	}
}

func TestParse_InvalidInstallType(t *testing.T) {
	yml := `
name: testpkg
version: "1.0"
url:
  linux_amd64: "https://example.com/internal"
install:
  type: magic
`
	_, err := Parse([]byte(yml))
	if err == nil {
		t.Fatal("expected validation error for invalid install type")
	}
}

func TestParse_UnsafeName(t *testing.T) {
	yml := `
name: "../evil"
version: "1.0"
url:
  linux_amd64: "https://example.com/internal"
install:
  type: binary
`
	_, err := Parse([]byte(yml))
	if err == nil {
		t.Fatal("expected error for unsafe name")
	}
}

func TestParse_HTTPURLRejected(t *testing.T) {
	yml := `
name: testpkg
version: "1.0"
url:
  linux_amd64: "http://example.com/internal"
install:
  type: binary
`
	_, err := Parse([]byte(yml))
	if err == nil {
		t.Fatal("expected error for HTTP URL")
	}
	if !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("error should mention HTTPS, got: %v", err)
	}
}

func TestParse_InvalidDependencyName(t *testing.T) {
	yml := `
name: testpkg
version: "1.0"
url:
  linux_amd64: "https://example.com/internal"
install:
  type: binary
dependencies:
  - "../escape"
`
	_, err := Parse([]byte(yml))
	if err == nil {
		t.Fatal("expected error for unsafe dependency name")
	}
}

func TestGetURL_CurrentPlatform(t *testing.T) {
	f := &Formula{
		Name: "test",
		URL: map[string]string{
			PlatformKey(): "https://example.com/test",
		},
	}
	u, err := f.GetURL()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != "https://example.com/test" {
		t.Errorf("url = %q, want %q", u, "https://example.com/test")
	}
}

func TestGetURL_RejectsHTTP(t *testing.T) {
	f := &Formula{
		Name: "test",
		URL: map[string]string{
			PlatformKey(): "http://example.com/test",
		},
	}
	_, err := f.GetURL()
	if err == nil {
		t.Fatal("expected error for HTTP URL")
	}
}

func TestGetURL_UnsupportedPlatform(t *testing.T) {
	f := &Formula{
		Name: "test",
		URL: map[string]string{
			"plan9_amd64": "https://example.com/test",
		},
	}
	_, err := f.GetURL()
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}

func TestGetSHA256_Valid(t *testing.T) {
	f := &Formula{
		Name: "test",
		SHA256: map[string]string{
			PlatformKey(): validSHA,
		},
	}
	sha, err := f.GetSHA256()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != validSHA {
		t.Errorf("sha256 = %q, want %q", sha, validSHA)
	}
}

func TestGetSHA256_InvalidHex(t *testing.T) {
	f := &Formula{
		Name: "test",
		SHA256: map[string]string{
			PlatformKey(): "not-a-valid-hex-string-at-all-needs-to-be-sixty-four-characters!",
		},
	}
	_, err := f.GetSHA256()
	if err == nil {
		t.Fatal("expected error for invalid SHA256")
	}
}

func TestValidateSHA256(t *testing.T) {
	if err := validation.ValidateSHA256(validSHA); err != nil {
		t.Errorf("valid SHA256 rejected: %v", err)
	}
	if err := validation.ValidateSHA256("too-short"); err == nil {
		t.Error("expected error for short SHA256")
	}
	if err := validation.ValidateSHA256("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"); err == nil {
		t.Error("expected error for non-hex SHA256")
	}
}

func TestPlatformKey(t *testing.T) {
	key := PlatformKey()
	expectedPrefix := runtime.GOOS + "_" + runtime.GOARCH
	if !strings.HasPrefix(key, expectedPrefix) {
		t.Errorf("PlatformKey() = %q, want prefix %q", key, expectedPrefix)
	}
}

func TestFilterByKind(t *testing.T) {
	deps := []Dependency{
		{Name: "a", Kind: DepRuntime},
		{Name: "b", Kind: DepBuild},
		{Name: "c", Kind: DepBuild},
		{Name: "d", Kind: DepTest},
	}
	got := FilterByKind(deps, DepBuild)
	if len(got) != 2 {
		t.Fatalf("want 2 build deps, got %d", len(got))
	}
	for _, d := range got {
		if d.Kind != DepBuild {
			t.Errorf("unexpected kind %v for dep %q", d.Kind, d.Name)
		}
	}
	if none := FilterByKind(deps, DepOptional); len(none) != 0 {
		t.Errorf("want 0 optional deps, got %d", len(none))
	}
}

func TestParse_StructuredDeps(t *testing.T) {
	yml := `
name: testpkg
version: "1.0.0"
description: "pkg"
homepage: "https://example.com"
license: "MIT"
url:
  darwin_arm64: "https://example.com/testpkg"
sha256:
  darwin_arm64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
install:
  type: binary
  binary_name: testpkg
deps:
  - name: libfoo
    kind: 0
  - name: cmake
    kind: 1
  - name: bats
    kind: 2
build_hooks:
  - configure
  - make
test_hook: run-tests
caveats: "Remember to set PATH."
`
	f, err := Parse([]byte(yml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Deps) != 3 {
		t.Fatalf("want 3 deps, got %d", len(f.Deps))
	}
	if f.Deps[1].Name != "cmake" || f.Deps[1].Kind != DepBuild {
		t.Errorf("unexpected dep[1]: %+v", f.Deps[1])
	}
	if len(f.BuildHooks) != 2 || f.BuildHooks[0] != "configure" {
		t.Errorf("unexpected BuildHooks: %v", f.BuildHooks)
	}
	if f.TestHook != "run-tests" {
		t.Errorf("TestHook = %q, want %q", f.TestHook, "run-tests")
	}
	if f.Caveats != "Remember to set PATH." {
		t.Errorf("Caveats = %q", f.Caveats)
	}
}

func TestParse_InvalidStructuredDepName(t *testing.T) {
	yml := `
name: testpkg
version: "1.0"
url:
  linux_amd64: "https://example.com/x"
install:
  type: binary
deps:
  - name: "../evil"
`
	_, err := Parse([]byte(yml))
	if err == nil {
		t.Fatal("expected error for unsafe dep name")
	}
}

func TestSortedMapKeys(t *testing.T) {
	m := map[string]string{"c": "3", "a": "1", "b": "2"}
	got := sortedMapKeys(m)
	if got != "a, b, c" {
		t.Errorf("sortedMapKeys = %q, want %q", got, "a, b, c")
	}
}

// A synthetic os/arch keeps these tests host-independent: GetPlatformKey only
// appends a macOS version suffix when the requested os/arch matches the host,
// so "plan9"/"ppc64" never gets a version suffix on any CI runner.
const fbOS, fbArch = "plan9", "ppc64"

func TestNewestVersionKey(t *testing.T) {
	f := &Formula{
		Name: "test",
		Bottle: map[string]BottleSpec{
			fbOS + "_" + fbArch + "_13": {URL: "https://example.com/v13", SHA256: validSHA},
			fbOS + "_" + fbArch + "_15": {URL: "https://example.com/v15", SHA256: validSHA},
			fbOS + "_" + fbArch + "_14": {URL: "https://example.com/v14", SHA256: validSHA},
		},
	}
	key, ok := f.newestVersionKey(fbOS, fbArch)
	if !ok {
		t.Fatal("expected a newest version key")
	}
	if want := fbOS + "_" + fbArch + "_15"; key != want {
		t.Errorf("newestVersionKey = %q, want %q", key, want)
	}
}

func TestResolveForceBottle_NewestVersionFallback(t *testing.T) {
	// No exact/generic key for the platform — only versioned keys exist, so
	// --force-bottle must fall back to the newest macOS version's bottle.
	f := &Formula{
		Name: "test",
		Bottle: map[string]BottleSpec{
			fbOS + "_" + fbArch + "_13": {URL: "https://example.com/v13", SHA256: validSHA},
			fbOS + "_" + fbArch + "_15": {URL: "https://example.com/v15", SHA256: validSHA, SHA512: validSHA512},
		},
	}
	url, sha256, sha512, err := f.resolveForceBottle(fbOS, fbArch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example.com/v15" {
		t.Errorf("url = %q, want newest (v15)", url)
	}
	if sha256 != validSHA {
		t.Errorf("sha256 = %q, want %q", sha256, validSHA)
	}
	if sha512 != validSHA512 {
		t.Errorf("sha512 = %q, want %q", sha512, validSHA512)
	}
}

func TestResolveForceBottle_PrefersGenericKey(t *testing.T) {
	// When a generic (non-versioned) bottle exists for the platform, normal
	// selection applies and the newest-version fallback is not used.
	f := &Formula{
		Name: "test",
		Bottle: map[string]BottleSpec{
			fbOS + "_" + fbArch:        {URL: "https://example.com/generic", SHA256: validSHA},
			fbOS + "_" + fbArch + "_9": {URL: "https://example.com/v9", SHA256: validSHA},
		},
	}
	url, _, _, err := f.resolveForceBottle(fbOS, fbArch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example.com/generic" {
		t.Errorf("url = %q, want generic", url)
	}
}

func TestResolveForceBottle_LegacyURLMap(t *testing.T) {
	// Legacy formulas store bottle URLs in the URL map; force-bottle must still
	// resolve the newest versioned key from there.
	f := &Formula{
		Name: "test",
		URL: map[string]string{
			fbOS + "_" + fbArch + "_14": "https://example.com/v14",
		},
		SHA256: map[string]string{
			fbOS + "_" + fbArch + "_14": validSHA,
		},
	}
	url, sha256, _, err := f.resolveForceBottle(fbOS, fbArch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://example.com/v14" {
		t.Errorf("url = %q, want v14", url)
	}
	if sha256 != validSHA {
		t.Errorf("sha256 = %q, want %q", sha256, validSHA)
	}
}

func TestResolveForceBottle_NoBottleErrors(t *testing.T) {
	// A source-only formula has no bottle: --force-bottle must error rather than
	// silently building from source.
	f := &Formula{
		Name:      "test",
		SourceURL: "https://example.com/test.tar.gz",
	}
	if _, _, _, err := f.resolveForceBottle(fbOS, fbArch); err == nil {
		t.Fatal("expected error when no bottle is available")
	}
}

func TestResolveForceBottle_RejectsHTTP(t *testing.T) {
	f := &Formula{
		Name: "test",
		Bottle: map[string]BottleSpec{
			fbOS + "_" + fbArch + "_15": {URL: "http://example.com/v15", SHA256: validSHA},
		},
	}
	if _, _, _, err := f.resolveForceBottle(fbOS, fbArch); err == nil {
		t.Fatal("expected error for insecure HTTP bottle URL")
	}
}
