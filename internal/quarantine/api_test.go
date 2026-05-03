package quarantine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApply_InvalidPath(t *testing.T) {
	err := Apply("/path/that/does/not/exist.app", "https://example.com/app.zip", "https://example.com")
	if err == nil {
		t.Fatal("expected error applying quarantine to non-existent path, got nil")
	}
}

func TestApply_Success(t *testing.T) {
	// Create a dummy file to quarantine
	tmpDir := t.TempDir()
	dummyPath := filepath.Join(tmpDir, "dummy.txt")
	if err := os.WriteFile(dummyPath, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}

	err := Apply(dummyPath, "https://example.com/app.zip", "https://example.com")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	// Verify the quarantine attribute was set using xattr
	out, err := exec.Command("xattr", "-p", "com.apple.quarantine", "--", dummyPath).CombinedOutput()
	if err != nil {
		t.Fatalf("failed to read quarantine attribute: %v (output: %s)", err, out)
	}

	// The value should look something like: 0081;...;grew;
	val := string(out)
	if !strings.Contains(val, "grew") {
		t.Errorf("expected quarantine value to contain 'grew', got: %s", val)
	}
}

func TestTrash_Empty(t *testing.T) {
	trashed, err := Trash()
	if err != nil {
		t.Fatalf("expected success with no paths, got error: %v", err)
	}
	if len(trashed) != 0 {
		t.Errorf("expected 0 trashed items, got %d", len(trashed))
	}
}

func TestTrash_InvalidPath(t *testing.T) {
	trashed, err := Trash("/path/that/definitely/does/not/exist/for/trashing")
	if err == nil {
		t.Fatal("expected error trashing non-existent path, got nil")
	}
	if len(trashed) != 0 {
		t.Errorf("expected 0 successfully trashed items, got %v", trashed)
	}
}

func TestTrash_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a couple of files to trash
	file1 := filepath.Join(tmpDir, "trash1.txt")
	file2 := filepath.Join(tmpDir, "trash2.txt")
	if err := os.WriteFile(file1, []byte("t1"), 0644); err != nil {
		t.Fatalf("failed to create file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("t2"), 0644); err != nil {
		t.Fatalf("failed to create file2: %v", err)
	}

	trashed, err := Trash(file1, file2)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(trashed) != 2 {
		t.Errorf("expected 2 trashed items, got %d: %v", len(trashed), trashed)
	}

	// Verify they are no longer at their original locations
	if _, err := os.Stat(file1); !os.IsNotExist(err) {
		t.Errorf("expected file1 to be gone, stat returned: %v", err)
	}
	if _, err := os.Stat(file2); !os.IsNotExist(err) {
		t.Errorf("expected file2 to be gone, stat returned: %v", err)
	}
}
