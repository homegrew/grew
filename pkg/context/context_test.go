package context

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/pkg/cask"
	"github.com/homegrew/grew/pkg/formula"
)

func TestNewLoader(t *testing.T) {
	tmpDir := t.TempDir()
	l := NewLoader(tmpDir)
	if l == nil {
		t.Fatal("expected loader to be non-nil")
	}
	if l.TapDir != tmpDir {
		t.Errorf("got tapDir %q, want %q", l.TapDir, tmpDir)
	}
	if l.DebugLog == nil {
		t.Error("expected DebugLog to be non-nil")
	}
}

func TestNewCaskLoader(t *testing.T) {
	tmpDir := t.TempDir()
	l := NewCaskLoader(tmpDir)
	if l == nil {
		t.Fatal("expected loader to be non-nil")
	}
	if l.TapDir != tmpDir {
		t.Errorf("got tapDir %q, want %q", l.TapDir, tmpDir)
	}
	if l.DebugLog == nil {
		t.Error("expected DebugLog to be non-nil")
	}
}

func TestNew(t *testing.T) {
	// Mock HOMEGREW_PREFIX, HOMEGREW_CACHE, and HOMEGREW_APPDIR to a temporary directory
	// to avoid affecting the host system and to avoid permission errors in restricted environments.
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

	// Create dummy .git directory in Taps to bypass git clone in tap.Manager.InitCore()
	tapsDir := filepath.Join(tmpDir, "Taps")
	if err := os.MkdirAll(filepath.Join(tapsDir, ".git"), 0755); err != nil {
		t.Fatalf("failed to create dummy .git dir: %v", err)
	}

	ctx, err := New()
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if ctx == nil {
		t.Fatal("expected context to be non-nil")
	}

	// Verify paths are correctly derived from our mocked prefix
	if ctx.Paths.Root != tmpDir {
		t.Errorf("got root %q, want %q", ctx.Paths.Root, tmpDir)
	}
	if ctx.Paths.Taps != tapsDir {
		t.Errorf("got taps %q, want %q", ctx.Paths.Taps, tapsDir)
	}

	// Verify components are initialized
	if ctx.Loader == nil {
		t.Error("expected Loader to be non-nil")
	}
	if ctx.CaskLoader == nil {
		t.Error("expected CaskLoader to be non-nil")
	}
	if ctx.Cellar == nil {
		t.Error("expected Cellar to be non-nil")
	}
	if ctx.Caskroom == nil {
		t.Error("expected Caskroom to be non-nil")
	}

	// Verify cellar and caskroom paths
	wantCellar := filepath.Join(tmpDir, "Cellar")
	if ctx.Cellar.Path != wantCellar {
		t.Errorf("got cellar path %q, want %q", ctx.Cellar.Path, wantCellar)
	}
	wantCaskroom := filepath.Join(tmpDir, "Caskroom")
	if ctx.Caskroom.Path != wantCaskroom {
		t.Errorf("got caskroom path %q, want %q", ctx.Caskroom.Path, wantCaskroom)
	}
}

func TestNew_Error(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file where the Taps directory should be to cause a failure
	tapsPath := filepath.Join(tmpDir, "Taps")
	if err := os.WriteFile(tapsPath, []byte("not a directory"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

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

	_, err := New()
	if err == nil {
		t.Error("expected New() to fail when Taps is a file, but it succeeded")
	}
}

func TestInstallContext(t *testing.T) {
	tmpDir := t.TempDir()

	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	origCache := os.Getenv("HOMEGREW_CACHE")
	origAppDir := os.Getenv("HOMEGREW_APPDIR")

	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	os.Setenv("HOMEGREW_CACHE", filepath.Join(tmpDir, "cache"))
	os.Setenv("HOMEGREW_APPDIR", filepath.Join(tmpDir, "Applications"))
	os.Setenv("HOMEGREW_NO_INIT_TAP", "1")

	defer func() {
		os.Setenv("HOMEGREW_PREFIX", origPrefix)
		os.Setenv("HOMEGREW_CACHE", origCache)
		os.Setenv("HOMEGREW_APPDIR", origAppDir)
		os.Unsetenv("HOMEGREW_NO_INIT_TAP")
	}()

	ictx, err := NewInstallContext()
	if err != nil {
		t.Fatalf("NewInstallContext() failed: %v", err)
	}
	defer ictx.Close()

	if ictx.GlobalLock == nil {
		t.Error("expected GlobalLock to be non-nil")
	}
	if ictx.Linker == nil {
		t.Error("expected Linker to be non-nil")
	}
	if ictx.DL == nil {
		t.Error("expected Downloader to be non-nil")
	}
	if ictx.AuditLog == nil {
		t.Error("expected AuditLog to be non-nil")
	}
}

func TestLoadFormula_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	os.Setenv("HOMEGREW_NO_INIT_TAP", "1")
	defer func() {
		os.Setenv("HOMEGREW_PREFIX", origPrefix)
		os.Unsetenv("HOMEGREW_NO_INIT_TAP")
	}()

	ctx, _ := New()
	_, err := ctx.LoadFormula("non-existent-formula")
	if err == nil {
		t.Error("expected error loading non-existent formula")
	}
}

func TestLoadCask_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	os.Setenv("HOMEGREW_NO_INIT_TAP", "1")
	defer func() {
		os.Setenv("HOMEGREW_PREFIX", origPrefix)
		os.Unsetenv("HOMEGREW_NO_INIT_TAP")
	}()

	ctx, _ := New()
	_, err := ctx.LoadCask("non-existent-cask")
	if err == nil {
		t.Error("expected error loading non-existent cask")
	}
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()

	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	os.Setenv("HOMEGREW_NO_INIT_TAP", "1")
	defer func() {
		os.Setenv("HOMEGREW_PREFIX", origPrefix)
		os.Unsetenv("HOMEGREW_NO_INIT_TAP")
	}()

	ictx, _ := NewInstallContext()
	lockFile := ictx.GlobalLock

	ictx.Close()

	if ictx.GlobalLock != nil {
		t.Error("expected GlobalLock to be nil after Close()")
	}

	// Verify that the file is closed by attempting to write to it (should fail)
	_, err := lockFile.WriteString("test")
	if err == nil {
		t.Error("expected write to closed lock file to fail")
	}
}

func ExampleContext() {
	// Context bundles commonly used objects like the formula loader and cellar.
	// It is typically initialized at the start of a command.

	// Use a temporary directory for the prefix to avoid permission errors
	// and side effects on the host system.
	tmpDir, err := os.MkdirTemp("", "homegrew-*")
	if err != nil {
		fmt.Printf("Error creating temp dir: %v\n", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	origPrefix := os.Getenv("HOMEGREW_PREFIX")
	origNoInit := os.Getenv("HOMEGREW_NO_INIT_TAP")
	os.Setenv("HOMEGREW_PREFIX", tmpDir)
	os.Setenv("HOMEGREW_NO_INIT_TAP", "1") // Skip tap initialization for example
	defer func() {
		os.Setenv("HOMEGREW_PREFIX", origPrefix)
		os.Setenv("HOMEGREW_NO_INIT_TAP", origNoInit)
	}()

	ctx, err := New()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	if ctx.Paths.Root == tmpDir {
		fmt.Println("Context initialized successfully")
	}

	// Output:
	// Context initialized successfully
}

func TestResolveKind(t *testing.T) {
	// Helper to create a fixture formula YAML file
	createFormulaFixture := func(tapDir, formulaName string) error {
		formulaDir := filepath.Join(tapDir, "homegrew", "homegrew-taps", "core")
		if err := os.MkdirAll(formulaDir, 0755); err != nil {
			return err
		}
		formulaYAML := fmt.Sprintf(`name: %s
version: "1.0"
homepage: https://example.com
description: test formula
url:
  darwin_arm64: https://example.com/%s-1.0.tar.gz
sha256:
  darwin_arm64: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
install:
  type: binary
  binary_name: %s
`, formulaName, formulaName, formulaName)
		return os.WriteFile(filepath.Join(formulaDir, formulaName+".yaml"), []byte(formulaYAML), 0644)
	}

	// Helper to create a fixture cask YAML file
	createCaskFixture := func(tapDir, caskName string) error {
		caskDir := filepath.Join(tapDir, "homegrew", "homegrew-taps", "cask")
		if err := os.MkdirAll(caskDir, 0755); err != nil {
			return err
		}
		caskYAML := fmt.Sprintf(`token: %s
version: "1.0"
url: https://example.com/%s-1.0.dmg
sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
name:
  - My %s
homepage: https://example.com
artifacts:
  - app: %s.app
`, caskName, caskName, caskName, caskName)
		return os.WriteFile(filepath.Join(caskDir, caskName+".yaml"), []byte(caskYAML), 0644)
	}

	tests := []struct {
		name         string
		forceCask    bool
		forceFormula bool
		setupFixture func(tapDir string) error
		packageName  string
		wantIsCask   bool
		wantErr      bool
	}{
		{
			name:         "ForceFormula_Found",
			forceCask:    false,
			forceFormula: true,
			setupFixture: func(tapDir string) error {
				return createFormulaFixture(tapDir, "myformula")
			},
			packageName: "myformula",
			wantIsCask:  false,
			wantErr:     false,
		},
		{
			name:         "ForceFormula_NotFound",
			forceCask:    false,
			forceFormula: true,
			setupFixture: func(tapDir string) error {
				return nil // No fixture
			},
			packageName: "nonexistent",
			wantIsCask:  false,
			wantErr:     true,
		},
		{
			name:         "ForceCask_Found",
			forceCask:    true,
			forceFormula: false,
			setupFixture: func(tapDir string) error {
				return createCaskFixture(tapDir, "mycask")
			},
			packageName: "mycask",
			wantIsCask:  true,
			wantErr:     false,
		},
		{
			name:         "ForceCask_NotFound",
			forceCask:    true,
			forceFormula: false,
			setupFixture: func(tapDir string) error {
				return nil // No fixture
			},
			packageName: "nonexistent",
			wantIsCask:  true,
			wantErr:     true,
		},
		{
			name:         "Auto_FormulaWins",
			forceCask:    false,
			forceFormula: false,
			setupFixture: func(tapDir string) error {
				if err := createFormulaFixture(tapDir, "both"); err != nil {
					return err
				}
				return createCaskFixture(tapDir, "both")
			},
			packageName: "both",
			wantIsCask:  false, // Formula should win
			wantErr:     false,
		},
		{
			name:         "Auto_CaskFallback",
			forceCask:    false,
			forceFormula: false,
			setupFixture: func(tapDir string) error {
				return createCaskFixture(tapDir, "onlycask")
			},
			packageName: "onlycask",
			wantIsCask:  true,
			wantErr:     false,
		},
		{
			name:         "Auto_NeitherFound",
			forceCask:    false,
			forceFormula: false,
			setupFixture: func(tapDir string) error {
				return nil // No fixture
			},
			packageName: "neither",
			wantIsCask:  false,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary tap directory for this test
			tapDir := t.TempDir()

			// Set up the fixture
			if err := tt.setupFixture(tapDir); err != nil {
				t.Fatalf("failed to set up fixture: %v", err)
			}

			// Create a minimal context with formula and cask loaders pointing to our temp tap
			ctx := &Context{
				Loader:     &formula.Loader{TapDir: tapDir},
				CaskLoader: &cask.Loader{TapDir: tapDir},
			}

			// Call ResolveKind
			isCask, err := ctx.ResolveKind(tt.packageName, tt.forceCask, tt.forceFormula)

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveKind(%q, %v, %v) error = %v, wantErr %v",
					tt.packageName, tt.forceCask, tt.forceFormula, err, tt.wantErr)
			}

			// Check isCask value (only meaningful if no error expected)
			if !tt.wantErr && isCask != tt.wantIsCask {
				t.Errorf("ResolveKind(%q, %v, %v) isCask = %v, want %v",
					tt.packageName, tt.forceCask, tt.forceFormula, isCask, tt.wantIsCask)
			}
		})
	}
}
