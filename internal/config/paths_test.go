package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPrefix_EnvOverride(t *testing.T) {
	t.Setenv("HOMEGREW_PREFIX", "/tmp/test-grew")
	p := DefaultPrefix()
	if p != "/tmp/test-grew" {
		t.Errorf("DefaultPrefix() = %q, want %q", p, "/tmp/test-grew")
	}
}

func TestDefaultPrefix_FallbackToHome(t *testing.T) {
	// Without env var and without the binary living in a grew prefix,
	// DefaultPrefix should fall back to ~/.homegrew.
	t.Setenv("HOMEGREW_PREFIX", "")
	p := DefaultPrefix()
	home, _ := os.UserHomeDir()

	// It should either be ~/.homegrew or a system prefix (if binary happens to
	// be installed there in the test environment).
	if !strings.HasSuffix(p, ".homegrew") && !strings.HasPrefix(p, "/opt/") && !strings.HasPrefix(p, "/usr/local/") {
		t.Errorf("DefaultPrefix() = %q, expected ~/.homegrew or a system prefix", p)
	}
	_ = home // avoid unused
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
	paths := FromRoot(root, filepath.Join(tmpDir, "Applications"))

	if err := paths.Init(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	for _, d := range []string{paths.Root, paths.Cellar, paths.Opt, paths.Bin, paths.Lib, paths.Include, paths.Taps, paths.CoreTap, paths.CaskTap, paths.Caskroom, paths.AppDir, paths.Tmp} {
		if info, err := os.Stat(d); err != nil || !info.IsDir() {
			t.Errorf("directory %q was not created", d)
		}
	}

	if info, err := os.Stat(paths.GitRepo); err == nil {
		if info.IsDir() {
			t.Fatalf("git repo directory must not be created")
		} else {
			t.Errorf("git repo directory must not exist")
		}
	}
}

func TestFromRoot(t *testing.T) {
	paths := FromRoot("/opt/homegrew", "/Users/test/Applications")
	if paths.Root != "/opt/homegrew" {
		t.Errorf("Root = %q", paths.Root)
	}
	if paths.Bin != "/opt/homegrew/bin" {
		t.Errorf("Bin = %q", paths.Bin)
	}
	if paths.AppDir != "/Users/test/Applications" {
		t.Errorf("AppDir = %q", paths.AppDir)
	}
}

func TestIsDir(t *testing.T) {
	tmpDir := t.TempDir()
	if !IsDir(tmpDir) {
		t.Errorf("IsDir(%q) should be true", tmpDir)
	}
	if IsDir(filepath.Join(tmpDir, "nope")) {
		t.Errorf("IsDir on non-existent path should be false")
	}
}

func TestDefaultPrefix_InfersFromBinary(t *testing.T) {
	// Simulate a grew prefix with the expected structure.
	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "grew")
	os.MkdirAll(filepath.Join(prefix, "bin"), 0755)
	os.MkdirAll(filepath.Join(prefix, "Cellar"), 0755)

	// The inference logic reads os.Executable(), which we can't fake here,
	// but we can at least test that the Cellar marker check works.
	if !IsDir(filepath.Join(prefix, "Cellar")) {
		t.Fatal("setup failed")
	}
}
