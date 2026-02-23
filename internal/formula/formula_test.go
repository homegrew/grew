package formula

import (
	"runtime"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/validation"
)

// validSHA is a valid 64-char hex SHA256 for tests.
const validSHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

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
	expected := runtime.GOOS + "_" + runtime.GOARCH
	if key != expected {
		t.Errorf("PlatformKey() = %q, want %q", key, expected)
	}
}

func TestSortedMapKeys(t *testing.T) {
	m := map[string]string{"c": "3", "a": "1", "b": "2"}
	got := sortedMapKeys(m)
	if got != "a, b, c" {
		t.Errorf("sortedMapKeys = %q, want %q", got, "a, b, c")
	}
}
