package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeatbeltProfile(t *testing.T) {
	cfg := BuildConfig{
		BuildDir: "/tmp/grew-build",
		KegDir:   "/tmp/grew-keg",
		DepPaths: []string{"/opt/openssl"},
	}
	profile := seatbeltProfile(cfg)

	checks := map[string]string{
		"(deny default)":     "deny by default",
		"(deny network*)":    "deny network",
		"(allow file-read*)": "allow all file reads",
		`(allow file-write* (subpath "/tmp/grew-build"))`:      "allow writes to build dir",
		`(allow file-write* (subpath "/tmp/grew-keg"))`:        "allow writes to keg dir",
		`(allow file-write* (subpath "/dev"))`:                 "allow writes to /dev",
		`(allow file-write* (subpath "/private/var/folders"))`: "allow writes to compiler cache",
	}
	for needle, desc := range checks {
		if !strings.Contains(profile, needle) {
			t.Errorf("profile must %s (missing %q)", desc, needle)
		}
	}

	for _, line := range strings.Split(profile, "\n") {
		if strings.TrimSpace(line) == "(allow file-write*)" {
			t.Error("profile must not have unrestricted file-write*")
		}
	}
}

func TestSandboxedEchoRunsDarwin(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}

	dir := t.TempDir()
	cfg := BuildConfig{BuildDir: dir, KegDir: dir}

	cmd := Command(cfg, "echo", "sandboxed")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sandboxed echo failed: %v", err)
	}
	if !strings.Contains(string(out), "sandboxed") {
		t.Errorf("expected 'sandboxed' in output, got %q", string(out))
	}
}

func TestSandboxDeniesNetworkDarwin(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}

	dir := t.TempDir()
	cfg := BuildConfig{BuildDir: dir, KegDir: dir}

	cmd := Command(cfg, "curl", "-s", "--max-time", "3", "https://example.com")
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		t.Error("expected network to be denied, but curl succeeded")
	}
}

func TestSandboxDeniesWriteOutsideBuildDirDarwin(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}

	buildDir := t.TempDir()
	kegDir := t.TempDir()
	cfg := BuildConfig{BuildDir: buildDir, KegDir: kegDir}

	target := "/usr/local/grew-sandbox-test-" + t.Name()
	defer os.Remove(target)

	cmd := Command(cfg, "touch", target)
	cmd.Dir = buildDir
	err := cmd.Run()
	if err == nil {
		if _, statErr := os.Stat(target); statErr == nil {
			os.Remove(target)
			t.Error("sandbox allowed writing outside build/keg dirs")
		}
	}
}

func TestSandboxAllowsWriteInBuildDirDarwin(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available")
	}

	buildDir := t.TempDir()
	kegDir := t.TempDir()
	cfg := BuildConfig{BuildDir: buildDir, KegDir: kegDir}

	target := filepath.Join(buildDir, "test-file")

	cmd := Command(cfg, "touch", target)
	cmd.Dir = buildDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox should allow writes inside build dir: %v\n%s", err, out)
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("file should exist after sandboxed touch in build dir")
	}
}
