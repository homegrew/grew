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

func TestDefaultPrefix_Fallback(t *testing.T) {
	t.Setenv("HOMEGREW_PREFIX", "")
	p := DefaultPrefix()

	if os.Geteuid() == 0 {
		// Root should always get the system prefix.
		if !strings.HasPrefix(p, "/opt/") && !strings.HasPrefix(p, "/usr/local/") {
			t.Errorf("DefaultPrefix() = %q, expected system prefix", p)
		}
	} else {
		// Non-root gets ~/.homegrew fallback (in production, runtime.Init
		// blocks before this is reached).
		if !strings.HasSuffix(p, ".homegrew") {
			t.Errorf("DefaultPrefix() = %q, expected ~/.homegrew for non-root", p)
		}
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
	paths := FromRoot(root, filepath.Join(tmpDir, "Applications"), filepath.Join(tmpDir, "Cache"))

	if err := paths.Init(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	for _, d := range []string{paths.Root, paths.Cellar, paths.Opt, paths.Bin, paths.Lib, paths.Include, paths.Taps, paths.Caskroom, paths.AppDir, paths.Cache, paths.Tmp} {
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
	paths := FromRoot("/opt/homegrew", "/Users/test/Applications", "/Users/test/Cache")
	if paths.Root != "/opt/homegrew" {
		t.Errorf("Root = %q", paths.Root)
	}
	if paths.Bin != "/opt/homegrew/bin" {
		t.Errorf("Bin = %q", paths.Bin)
	}
	if paths.AppDir != "/Users/test/Applications" {
		t.Errorf("AppDir = %q", paths.AppDir)
	}
	if paths.Cache != "/Users/test/Cache" {
		t.Errorf("Cache = %q", paths.Cache)
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

func TestIsUnderRoot(t *testing.T) {
	p := Paths{Root: "/opt/homegrew"}
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homegrew/bin/grew", true},
		{"/opt/homegrew", true},
		{"/usr/local/bin", false},
		{"/opt/homegrew/../other", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := p.IsUnderRoot(tt.path); got != tt.want {
			t.Errorf("IsUnderRoot(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFromRoot_InvalidPaths(t *testing.T) {
	// Relative path should be made absolute.
	cwd, _ := os.Getwd()
	p := FromRoot("myroot", "myapps", "mycache")
	
	if !filepath.IsAbs(p.Root) {
		t.Errorf("Root should be absolute, got %q", p.Root)
	}
	if p.Root != filepath.Join(cwd, "myroot") {
		t.Errorf("Root mismatch: got %q, want %q", p.Root, filepath.Join(cwd, "myroot"))
	}

	// The root path "/" is rejected by SafeAbsolutePath, triggering fallback.
	p2 := FromRoot("/", "apps", "cache")
	if !strings.Contains(p2.Root, "homegrew") {
		t.Errorf("Expected fallback for illegal root, got %q", p2.Root)
	}
}

func TestSystemPrefix(t *testing.T) {
	p := systemPrefix()
	if p == "" {
		t.Fatal("systemPrefix returned empty string")
	}
	if !strings.Contains(p, "homegrew") {
		t.Errorf("systemPrefix %q doesn't contain homegrew", p)
	}
}

func TestDefault_EnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "Apps")
	cacheDir := filepath.Join(tmpDir, "Cache")
	
	t.Setenv("HOMEGREW_APPDIR", appDir)
	t.Setenv("HOMEGREW_CACHE", cacheDir)
	
	paths := Default()
	if paths.AppDir != appDir {
		t.Errorf("AppDir override failed: got %q, want %q", paths.AppDir, appDir)
	}
	if paths.Cache != cacheDir {
		t.Errorf("Cache override failed: got %q, want %q", paths.Cache, cacheDir)
	}
}

func TestInit_OutsideRoot(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	// Attempt to set a path outside the root.
	paths := FromRoot(root, "/tmp/illegal-apps", "/tmp/illegal-cache")
	
	// Paths.Init should allow AppDir and Cache outside root, but let's test a case that shouldn't be allowed
	// Actually Init hardcodes which ones to check.
	
	// Manually corrupt a path to be outside root but not in the allowed list (AppDir/Cache)
	paths.Bin = "/usr/bin" 
	
	err := paths.Init()
	if err == nil {
		t.Error("Expected error when initializing path outside root")
	}
}

func TestDefaultPrefix_InvalidEnv(t *testing.T) {
	// Root "/" is invalid according to SafeAbsolutePath.
	t.Setenv("HOMEGREW_PREFIX", "/")
	p := DefaultPrefix()
	if strings.HasSuffix(p, "/") {
		t.Errorf("DefaultPrefix() returned invalid root %q", p)
	}
}

func TestFromRoot_Fallbacks(t *testing.T) {
	// Test fallback for invalid AppDir and CacheDir
	// "/" is rejected by SafeAbsolutePath
	p := FromRoot("/opt/homegrew", "/", "/")
	
	if !strings.HasSuffix(p.AppDir, "Applications") {
		t.Errorf("AppDir fallback failed: %q", p.AppDir)
	}
	if !strings.Contains(p.Cache, "homegrew") {
		t.Errorf("Cache fallback failed: %q", p.Cache)
	}
}

func TestDefault_InvalidAppDirEnv(t *testing.T) {
	// "/" is rejected by SafeAbsolutePath
	t.Setenv("HOMEGREW_APPDIR", "/")
	paths := Default()
	if !strings.HasSuffix(paths.AppDir, "Applications") {
		t.Errorf("AppDir env fallback failed: %q", paths.AppDir)
	}
}

func TestIsDir_Invalid(t *testing.T) {
	if IsDir("") {
		t.Error("IsDir(\"\") should be false")
	}
	if IsDir("/../../invalid") {
		t.Error("IsDir with unsafe path should be false")
	}
}

