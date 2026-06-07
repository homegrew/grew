package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests (run on any platform)
// ---------------------------------------------------------------------------

func TestCleanEnv(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "hunter2")
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("CC", "clang")
	t.Setenv("PATH", "/usr/bin")

	cfg := BuildConfig{BuildDir: t.TempDir(), KegDir: t.TempDir()}
	env := cleanEnv(cfg)
	envMap := make(map[string]string)
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		envMap[k] = v
	}

	if envMap["CC"] != "clang" {
		t.Errorf("expected CC=clang, got %q", envMap["CC"])
	}
	if envMap["PATH"] != "/usr/bin" {
		t.Errorf("expected PATH=/usr/bin, got %q", envMap["PATH"])
	}
	if _, ok := envMap["AWS_SECRET_ACCESS_KEY"]; ok {
		t.Error("AWS_SECRET_ACCESS_KEY should be stripped")
	}
	if _, ok := envMap["GITHUB_TOKEN"]; ok {
		t.Error("GITHUB_TOKEN should be stripped")
	}
	if !strings.HasPrefix(envMap["TMPDIR"], cfg.BuildDir) {
		t.Errorf("TMPDIR should be under build dir, got %q", envMap["TMPDIR"])
	}
}

func TestCleanEnvTMPDIRCreated(t *testing.T) {
	buildDir := t.TempDir()
	cfg := BuildConfig{BuildDir: buildDir, KegDir: t.TempDir()}
	cleanEnv(cfg)

	tmpDir := filepath.Join(buildDir, ".grew-tmp")
	info, err := os.Stat(tmpDir)
	if err != nil {
		t.Fatalf("TMPDIR not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("TMPDIR should be a directory")
	}
}

func TestCommandReturnsNonNil(t *testing.T) {
	cfg := BuildConfig{
		BuildDir: t.TempDir(),
		KegDir:   t.TempDir(),
		DepPaths: []string{"/opt/dep1"},
	}
	cmd := Command(cfg, "echo", "hello")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if len(cmd.Env) == 0 {
		t.Error("expected clean env to be set")
	}
}
