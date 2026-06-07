package installer

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/homegrew/grew/pkg/release"
)

// writeScript creates an executable shell script at path with the given content.
func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on windows")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("#!/bin/sh\n" + content)); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBinaryIntegrity_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `echo "grew 1.2.3"`)

	if err := VerifyBinaryIntegrity(bin, "1.2.3"); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestVerifyBinaryIntegrity_SuccessNoExpectedVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `echo "grew 0.0.1-dev"`)

	// Empty expectedVersion = git build, just check it runs.
	if err := VerifyBinaryIntegrity(bin, ""); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestVerifyBinaryIntegrity_VersionMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `echo "grew 0.0.1"`)

	err := VerifyBinaryIntegrity(bin, "2.0.0")
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
	if got := err.Error(); !contains(got, "version mismatch") {
		t.Errorf("expected 'version mismatch' in error, got: %s", got)
	}
}

func TestVerifyBinaryIntegrity_BinaryNotFound(t *testing.T) {
	t.Parallel()
	err := VerifyBinaryIntegrity("/nonexistent/grew", "1.0.0")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestVerifyBinaryIntegrity_BinaryFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `exit 1`)

	err := VerifyBinaryIntegrity(bin, "1.0.0")
	if err == nil {
		t.Fatal("expected error for failing binary")
	}
	if got := err.Error(); !contains(got, "failed to execute") {
		t.Errorf("expected 'failed to execute' in error, got: %s", got)
	}
}

func TestVerifyBinaryIntegrity_EmptyOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `true`) // produces no output

	err := VerifyBinaryIntegrity(bin, "")
	if err == nil {
		t.Fatal("expected error for empty output")
	}
	if got := err.Error(); !contains(got, "no version output") {
		t.Errorf("expected 'no version output' in error, got: %s", got)
	}
}

func TestVerifyBinaryIntegrity_VPrefixHandled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `echo "grew v3.0.0"`)

	// Expected version without "v" prefix should still match.
	if err := VerifyBinaryIntegrity(bin, "3.0.0"); err != nil {
		t.Fatalf("expected success with v-prefix, got: %v", err)
	}
}

func TestVerifyBinaryIntegrity_SingleWordVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	// Binary outputs just the version, no "grew " prefix.
	writeScript(t, bin, `echo "4.0.0"`)

	if err := VerifyBinaryIntegrity(bin, "4.0.0"); err != nil {
		t.Fatalf("expected success with single-word version, got: %v", err)
	}
}

func TestFileHashes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test")
	content := []byte("hello world\n")
	os.WriteFile(path, content, 0644)

	got256, got512, err := FileHashes(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h256 := sha256.Sum256(content)
	want256 := hex.EncodeToString(h256[:])
	if got256 != want256 {
		t.Errorf("got SHA-256 %s, want %s", got256, want256)
	}

	h512 := sha512.Sum512(content)
	want512 := hex.EncodeToString(h512[:])
	if got512 != want512 {
		t.Errorf("got SHA-512 %s, want %s", got512, want512)
	}
}

func TestFileSHA256(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test")
	content := []byte("hello world\n")
	os.WriteFile(path, content, 0644)

	got, err := release.FileSHA256(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestFileSHA256_NotExist(t *testing.T) {
	t.Parallel()
	_, err := release.FileSHA256("/nonexistent/file")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFileSHA256_Deterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	os.WriteFile(path, []byte("same content"), 0644)

	h1, _ := release.FileSHA256(path)
	h2, _ := release.FileSHA256(path)
	if h1 != h2 {
		t.Errorf("same file produced different hashes:\n  %s\n  %s", h1, h2)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
