package tap

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseVerifyMode(t *testing.T) {
	tests := []struct {
		input string
		want  VerifyMode
	}{
		{"", VerifyOff},
		{"off", VerifyOff},
		{"warn", VerifyWarn},
		{"WARN", VerifyWarn},
		{"strict", VerifyStrict},
		{"STRICT", VerifyStrict},
		{" strict ", VerifyStrict},
		{"invalid", VerifyOff},
	}
	for _, tt := range tests {
		got := ParseVerifyMode(tt.input)
		if got != tt.want {
			t.Errorf("ParseVerifyMode(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestVerifyHeadSignature_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	err := VerifyHeadSignature(dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestVerifyHeadSignature_UnsignedCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a git repo with an unsigned commit.
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s", args, out)
		}
	}

	run("init")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644)
	run("add", ".")
	run("commit", "-m", "unsigned commit")

	err := VerifyHeadSignature(dir)
	if err == nil {
		t.Fatal("expected error for unsigned commit")
	}
}

func TestCheckAfterUpdate_Off(t *testing.T) {
	// Should always return nil regardless of directory.
	err := CheckAfterUpdate("/nonexistent", VerifyOff)
	if err != nil {
		t.Fatalf("expected nil for VerifyOff, got %v", err)
	}
}

func TestCheckAfterUpdate_Warn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s", args, out)
		}
	}

	run("init")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644)
	run("add", ".")
	run("commit", "-m", "unsigned commit")

	// Warn mode should NOT return an error.
	err := CheckAfterUpdate(dir, VerifyWarn)
	if err != nil {
		t.Fatalf("expected nil for VerifyWarn with unsigned commit, got %v", err)
	}
}

func TestCheckAfterUpdate_Strict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s", args, out)
		}
	}

	run("init")
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644)
	run("add", ".")
	run("commit", "-m", "unsigned commit")

	// Strict mode SHOULD return an error.
	err := CheckAfterUpdate(dir, VerifyStrict)
	if err == nil {
		t.Fatal("expected error for VerifyStrict with unsigned commit")
	}
}
