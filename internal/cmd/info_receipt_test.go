package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homegrew/grew/internal/context"
	"github.com/homegrew/grew/internal/receipt"
)

func TestRunInfoWithReceipt(t *testing.T) {
	tmpDir := t.TempDir()
	
	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	defer os.Setenv("HOMEGREW_PREFIX", origPrefix)

	// Setup environment
	coreTapDir := filepath.Join(tmpDir, "Taps", "homegrew", "homegrew-taps", "core")
	os.MkdirAll(coreTapDir, 0755)
	os.MkdirAll(filepath.Join(tmpDir, "Taps", "homegrew", "homegrew-taps", ".git"), 0755)

	formulaName := "test-formula"
	formulaPath := filepath.Join(coreTapDir, formulaName+".yaml")
	formulaYAML := `name: test-formula
version: 1.0.0
description: A test formula
homepage: https://example.com
license: MIT
url:
  darwin_arm64: https://example.com/test-formula-1.0.0.tar.gz
sha256:
  darwin_arm64: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
`
	os.WriteFile(formulaPath, []byte(formulaYAML), 0644)

	// Create "installed" formula
	kegPath := filepath.Join(tmpDir, "Cellar", formulaName, "1.0.0")
	os.MkdirAll(kegPath, 0755)

	// Create receipt
	now := time.Now().Round(time.Second)
	r := &receipt.Receipt{
		Name:             formulaName,
		Version:          "1.0.0",
		PouredFromBottle: true,
		InstalledAt:      now,
	}
	if err := receipt.Save(r, kegPath); err != nil {
		t.Fatal(err)
	}

	ctx, err := context.New()
	if err != nil {
		t.Fatal(err)
	}

	// Capture stdout for runInfo
	oldStdout := os.Stdout
	readPipe, writePipe, _ := os.Pipe()
	os.Stdout = writePipe

	err = runInfo([]string{formulaName})
	writePipe.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runInfo failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(readPipe)
	output := buf.String()

	expectedDate := now.Format("2006-01-02 at 15:04:05")
	if !strings.Contains(output, "Poured from bottle on "+expectedDate) {
		t.Errorf("expected output to contain receipt info, got:\n%s", output)
	}

	// Test JSON output
	readPipe, writePipe, _ = os.Pipe()
	os.Stdout = writePipe

	err = runInfoJSON(ctx, []string{formulaName}, false)
	writePipe.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runInfoJSON failed: %v", err)
	}

	buf.Reset()
	buf.ReadFrom(readPipe)
	
	var jsonOutput InfoJSONv2
	if err := json.Unmarshal(buf.Bytes(), &jsonOutput); err != nil {
		t.Fatal(err)
	}

	if len(jsonOutput.Formulae) == 0 || len(jsonOutput.Formulae[0].Installed) == 0 {
		t.Fatal("expected installed formula in JSON output")
	}

	inst := jsonOutput.Formulae[0].Installed[0]
	if inst.BuiltFromSource != false {
		t.Errorf("expected BuiltFromSource to be false, got %v", inst.BuiltFromSource)
	}
	if inst.InstalledAt != now.Format(time.RFC3339) {
		t.Errorf("expected InstalledAt %s, got %s", now.Format(time.RFC3339), inst.InstalledAt)
	}
}
