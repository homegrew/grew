package context

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
