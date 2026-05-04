package info

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/context"
)

func TestRunInfoJSON(t *testing.T) {
	tmpDir := t.TempDir()
	
	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	origCache := os.Getenv("HOMEGREW_CACHE")
	origAppDir := os.Getenv("HOMEGREW_APPDIR")
	
	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	os.Setenv("HOMEGREW_CACHE", filepath.Join(tmpDir, "cache"))
	os.Setenv("HOMEGREW_APPDIR", filepath.Join(tmpDir, "Applications"))
	
	defer func() {
		os.Setenv("HOMEGREW_PREFIX", origPrefix)
		os.Setenv("HOMEGREW_CACHE", origCache)
		os.Setenv("HOMEGREW_APPDIR", origAppDir)
	}()

	// Create dummy core tap
	coreTapDir := filepath.Join(tmpDir, "Taps", "homegrew", "homegrew-taps", "core")
	if err := os.MkdirAll(coreTapDir, 0755); err != nil {
		t.Fatal(err)
	}
	
	// Also create .git so New() doesn't try to clone
	if err := os.MkdirAll(filepath.Join(tmpDir, "Taps", "homegrew", "homegrew-taps", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	formulaPath := filepath.Join(coreTapDir, "test-formula.yaml")
	formulaYAML := `name: test-formula
version: 1.0.0
description: A test formula
homepage: https://example.com
license: MIT
url:
  darwin_arm64: https://example.com/test-formula-1.0.0.tar.gz
sha256:
  darwin_arm64: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
install:
  type: binary
`
	if err := os.WriteFile(formulaPath, []byte(formulaYAML), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, err := context.New()
	if err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = w.Close()
		_ = r.Close()
	}()

	err = runInfoJSON(ctx, []string{"test-formula"}, false)
	w.Close()

	if err != nil {
		t.Fatalf("runInfoJSON failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	
	var output InfoJSONv2
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v\nOutput was: %s", err, buf.String())
	}

	if len(output.Formulae) != 1 {
		t.Errorf("expected 1 formula, got %d", len(output.Formulae))
	}
	if output.Formulae[0].Name != "test-formula" {
		t.Errorf("expected formula name test-formula, got %s", output.Formulae[0].Name)
	}
	if output.Formulae[0].Versions.Stable != "1.0.0" {
		t.Errorf("expected stable version 1.0.0, got %s", output.Formulae[0].Versions.Stable)
	}
}

func TestRunCaskInfoJSON(t *testing.T) {
	tmpDir := t.TempDir()
	
	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	origCache := os.Getenv("HOMEGREW_CACHE")
	origAppDir := os.Getenv("HOMEGREW_APPDIR")
	
	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	os.Setenv("HOMEGREW_CACHE", filepath.Join(tmpDir, "cache"))
	os.Setenv("HOMEGREW_APPDIR", filepath.Join(tmpDir, "Applications"))
	
	defer func() {
		os.Setenv("HOMEGREW_PREFIX", origPrefix)
		os.Setenv("HOMEGREW_CACHE", origCache)
		os.Setenv("HOMEGREW_APPDIR", origAppDir)
	}()

	// Create dummy cask tap
	caskTapDir := filepath.Join(tmpDir, "Taps", "homegrew", "homegrew-taps", "cask")
	if err := os.MkdirAll(caskTapDir, 0755); err != nil {
		t.Fatal(err)
	}
	
	// Also create .git so New() doesn't try to clone
	if err := os.MkdirAll(filepath.Join(tmpDir, "Taps", "homegrew", "homegrew-taps", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	caskPath := filepath.Join(caskTapDir, "test-cask.yaml")
	caskYAML := `name: test-cask
version: 2.0.0
description: A test cask
homepage: https://example.com/cask
url:
  darwin_arm64: https://example.com/test-cask-2.0.0.zip
sha256:
  darwin_arm64: fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
artifacts:
  app:
    - Test.app
`
	if err := os.WriteFile(caskPath, []byte(caskYAML), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, err := context.New()
	if err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = w.Close()
		_ = r.Close()
	}()

	err = runInfoJSON(ctx, []string{"test-cask"}, true)
	w.Close()

	if err != nil {
		t.Fatalf("runInfoJSON failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	
	var output InfoJSONv2
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v\nOutput was: %s", err, buf.String())
	}

	if len(output.Casks) != 1 {
		t.Errorf("expected 1 cask, got %d", len(output.Casks))
	}
	if output.Casks[0].Token != "test-cask" {
		t.Errorf("expected cask token test-cask, got %s", output.Casks[0].Token)
	}
	if output.Casks[0].Version != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", output.Casks[0].Version)
	}
	if len(output.Casks[0].Artifacts) == 0 || len(output.Casks[0].Artifacts[0].App) != 1 {
		t.Errorf("expected 1 app artifact, got %v", output.Casks[0].Artifacts)
	}
}
