package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault_UsesHomeDir(t *testing.T) {
	os.Unsetenv("HOMEGREW_PREFIX")
	paths := Default()
	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(paths.Root, home) {
		t.Errorf("root %q should be under home %q", paths.Root, home)
	}
	if !strings.HasSuffix(paths.Root, ".grew") {
		t.Errorf("root %q should end with .grew", paths.Root)
	}
}

func TestDefault_OverridePrefix(t *testing.T) {
	t.Setenv("HOMEGREW_PREFIX", "/tmp/test-grew")
	paths := Default()
	if paths.Root != "/tmp/test-grew" {
		t.Errorf("root = %q, want %q", paths.Root, "/tmp/test-grew")
	}
	if paths.Cellar != "/tmp/test-grew/Cellar" {
		t.Errorf("cellar = %q, want %q", paths.Cellar, "/tmp/test-grew/Cellar")
	}
}

func TestInit_CreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "grew")
	paths := Paths{
		Root:     root,
		Cellar:   filepath.Join(root, "Cellar"),
		Opt:      filepath.Join(root, "opt"),
		Bin:      filepath.Join(root, "bin"),
		Lib:      filepath.Join(root, "lib"),
		Include:  filepath.Join(root, "include"),
		Taps:     filepath.Join(root, "Taps"),
		CoreTap:  filepath.Join(root, "Taps", "core"),
		CaskTap:  filepath.Join(root, "Taps", "cask"),
		Caskroom: filepath.Join(root, "Caskroom"),
		AppDir:   filepath.Join(tmpDir, "Applications"),
		Tmp:      filepath.Join(root, "tmp"),
	}

	if err := paths.Init(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	for _, d := range []string{paths.Root, paths.Cellar, paths.Opt, paths.Bin, paths.Lib, paths.Include, paths.Taps, paths.CoreTap, paths.CaskTap, paths.Caskroom, paths.AppDir, paths.Tmp} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("directory %q was not created", d)
		}
	}
}
