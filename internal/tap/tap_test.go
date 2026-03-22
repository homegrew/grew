package tap

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result string)
	}{
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "valid absolute",
			input:   "/tmp/taps",
			wantErr: false,
			check: func(t *testing.T, result string) {
				if !filepath.IsAbs(result) {
					t.Errorf("expected absolute path, got %q", result)
				}
			},
		},
		{
			name:    "valid relative becomes absolute",
			input:   "taps",
			wantErr: false,
			check: func(t *testing.T, result string) {
				if !filepath.IsAbs(result) {
					t.Errorf("expected absolute path, got %q", result)
				}
			},
		},
		{
			name:    "dash prefix rejected",
			input:   "/tmp/-evil",
			wantErr: true,
		},
		{
			name:    "dash prefix nested ok",
			input:   "/tmp/ok/-nested",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := cleanDir(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestEnsureCloned_AlreadyCloned(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	gitInit(t, dir)

	mgr := &Manager{TapsDir: dir}
	if err := mgr.EnsureCloned(); err != nil {
		t.Fatalf("EnsureCloned should succeed for existing repo: %v", err)
	}
}

func TestEnsureCloned_InvalidDir(t *testing.T) {
	t.Parallel()
	mgr := &Manager{TapsDir: ""}
	if err := mgr.EnsureCloned(); err == nil {
		t.Fatal("expected error for empty TapsDir")
	}
}

func TestUpdate_CountsFormulas(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a git repo simulating a tap structure with core/ and cask/ files.
	dir := t.TempDir()
	gitInit(t, dir)

	coreDir := filepath.Join(dir, "core")
	caskDir := filepath.Join(dir, "cask")
	os.MkdirAll(coreDir, 0755)
	os.MkdirAll(caskDir, 0755)

	os.WriteFile(filepath.Join(coreDir, "jq.yaml"), []byte("name: jq"), 0644)
	os.WriteFile(filepath.Join(coreDir, "curl.yaml"), []byte("name: curl"), 0644)
	os.WriteFile(filepath.Join(caskDir, "firefox.yaml"), []byte("name: firefox"), 0644)

	gitAdd(t, dir)
	gitCommit(t, dir, "add formulas")

	// Create a bare remote so fetch/reset works.
	// Use dir as the working directory for git commands — never leave
	// cmd.Dir unset, because the default (package source dir) can be
	// removed by cleanup or parallel tests, causing flaky failures.
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, dir, "clone", "--bare", dir, remote)

	// Re-clone from the bare remote so the repo has an origin.
	cloned := filepath.Join(t.TempDir(), "cloned")
	runGit(t, dir, "clone", remote, cloned)

	// Copy formula files into the clone (they're already there from clone).
	mgr := &Manager{TapsDir: cloned}
	count, err := mgr.Update()
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// 2 core + 1 cask = 3 formulas.
	if count != 3 {
		t.Errorf("Update returned count=%d, want 3", count)
	}
}

func TestVerifyTagSignature_InvalidTags(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	gitInit(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0644)
	gitAdd(t, dir)
	gitCommit(t, dir, "init")

	tests := []struct {
		name string
		tag  string
	}{
		{"starts with dash", "-evil"},
		{"contains slash", "a/b"},
		{"contains backslash", `a\b`},
		{"contains dot", "a.b"},
		{"contains space", "a b"},
		{"contains tab", "a\tb"},
		{"contains newline", "a\nb"},
		{"contains null", "a\x00b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := VerifyTagSignature(dir, tt.tag)
			if err == nil {
				t.Errorf("expected error for tag %q", tt.tag)
			}
			if err != nil && !strings.Contains(err.Error(), "invalid tag name") {
				t.Errorf("expected 'invalid tag name' error, got: %v", err)
			}
		})
	}
}

func TestVerifyTagSignature_NotGitRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := VerifyTagSignature(dir, "v1.0")
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestCheckAfterUpdate_OffAlwaysNil(t *testing.T) {
	t.Parallel()
	// VerifyOff should return nil even for a nonsensical path.
	if err := CheckAfterUpdate("/this/does/not/exist", VerifyOff); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// --- helpers ---

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %s (%v)", args, out, err)
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "-b", "main")
	// Create an initial commit so HEAD exists.
	os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte{}, 0644)
	gitAdd(t, dir)
	gitCommit(t, dir, "initial")
}

func gitAdd(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "add", ".")
}

func gitCommit(t *testing.T, dir string, msg string) {
	t.Helper()
	runGit(t, dir, "commit", "-m", msg)
}
