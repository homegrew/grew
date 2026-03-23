package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	os.WriteFile(src, []byte("hello"), 0644)

	if err := CopyFile(src, dst, 0755); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want %q", data, "hello")
	}

	info, _ := os.Stat(dst)
	if info.Mode().Perm()&0100 == 0 {
		t.Error("expected executable permission on dst")
	}
}

func TestCopyFile_SrcNotExist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := CopyFile(filepath.Join(dir, "nope"), filepath.Join(dir, "dst"), 0644)
	if err == nil {
		t.Fatal("expected error for missing src")
	}
}

func TestCopyFileWithinRoot_EmptySrc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := CopyFileWithinRoot("", filepath.Join(dir, "dst"), dir, 0644)
	if err == nil {
		t.Fatal("expected error for empty src")
	}
}

func TestCopyFileWithinRoot_EmptyDst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	err := CopyFileWithinRoot(src, "", dir, 0644)
	if err == nil {
		t.Fatal("expected error for empty dst")
	}
}

func TestCopyFileWithinRoot_SrcTraversal(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Write a real file in srcDir.
	realFile := filepath.Join(srcDir, "real.txt")
	if err := os.WriteFile(realFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	// A relative src path that traverses into srcDir via "..".
	// e.g. "<dstDir>/../<srcDir basename>/real.txt" — after Abs+Clean this
	// resolves to the real absolute path so the copy should succeed.
	relSrc := filepath.Join(dstDir, "..", filepath.Base(srcDir), "real.txt")
	dst := filepath.Join(dstDir, "out.txt")

	if err := CopyFileWithinRoot(relSrc, dst, dstDir, 0644); err != nil {
		t.Fatalf("expected copy to succeed after path normalization: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want %q", string(data), "hello")
	}
}

func TestCopyTree(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dest")

	// Build a source tree.
	os.MkdirAll(filepath.Join(src, "bin"), 0755)
	os.WriteFile(filepath.Join(src, "bin", "tool"), []byte("binary"), 0755)
	os.WriteFile(filepath.Join(src, "README"), []byte("readme"), 0644)

	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	// Verify files copied.
	data, err := os.ReadFile(filepath.Join(dst, "bin", "tool"))
	if err != nil {
		t.Fatalf("read bin/tool: %v", err)
	}
	if string(data) != "binary" {
		t.Errorf("bin/tool = %q, want %q", data, "binary")
	}

	data, err = os.ReadFile(filepath.Join(dst, "README"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if string(data) != "readme" {
		t.Errorf("README = %q, want %q", data, "readme")
	}
}

func TestCopyTree_PreservesSymlinks(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dest")

	os.WriteFile(filepath.Join(src, "real.txt"), []byte("data"), 0644)
	os.Symlink("real.txt", filepath.Join(src, "link.txt"))

	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	target, err := os.Readlink(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "real.txt" {
		t.Errorf("symlink target = %q, want %q", target, "real.txt")
	}
}

func TestCopyTree_SkipsEscapingSymlinks(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dest")

	os.WriteFile(filepath.Join(src, "safe.txt"), []byte("ok"), 0644)
	os.Symlink("/etc/passwd", filepath.Join(src, "escape"))

	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}

	// safe.txt should be copied.
	if _, err := os.Stat(filepath.Join(dst, "safe.txt")); err != nil {
		t.Error("safe.txt should have been copied")
	}

	// escape symlink should be skipped.
	if _, err := os.Lstat(filepath.Join(dst, "escape")); !os.IsNotExist(err) {
		t.Error("escaping symlink should have been skipped")
	}
}

func TestSanitizeMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		mode  os.FileMode
		isDir bool
		want  os.FileMode
	}{
		{"file zero", 0, false, 0644},
		{"file normal", 0644, false, 0644},
		{"file executable", 0755, false, 0755},
		{"file setuid stripped", os.ModeSetuid | 0755, false, 0755},
		{"file setgid stripped", os.ModeSetgid | 0755, false, 0755},
		{"file sticky stripped", os.ModeSticky | 0755, false, 0755},
		{"file world-write stripped", 0777, false, 0775},
		{"dir zero", 0, true, 0755},
		{"dir normal", 0755, true, 0755},
		{"dir needs owner exec", 0644, true, 0744},
		{"dir world-write stripped", 0777, true, 0775},
		{"dir setuid stripped", os.ModeSetuid | 0755, true, 0755},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeMode(tt.mode, tt.isDir)
			if got != tt.want {
				t.Errorf("SanitizeMode(%o, %v) = %o, want %o", tt.mode, tt.isDir, got, tt.want)
			}
		})
	}
}
