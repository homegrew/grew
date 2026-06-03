package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCache_Dir(t *testing.T) {
	t.Parallel()
	dir := "/tmp/grew-cache"
	c := New(dir)
	if actual := c.Dir(); actual != dir {
		t.Errorf("expected %q, got %q", dir, actual)
	}
}

func TestCache_DownloadsDir(t *testing.T) {
	t.Parallel()
	c := New("/tmp/grew-cache")
	expected := filepath.Join("/tmp/grew-cache", "downloads")
	if actual := c.DownloadsDir(); actual != expected {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func TestCache_DownloadPath(t *testing.T) {
	t.Parallel()
	c := New("/tmp/grew-cache")
	
	// Valid filename
	path, err := c.DownloadPath("test-file.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("/tmp/grew-cache", "downloads", "test-file.tar.gz")
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}

	// Invalid filename (path traversal)
	_, err = c.DownloadPath("../test-file.tar.gz")
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}

	// Missing cache dir
	emptyCache := New("")
	_, err = emptyCache.DownloadPath("test.txt")
	if err == nil {
		t.Error("expected error for empty cache dir, got nil")
	}
}

func TestCache_ExistsAndStore(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	c := New(filepath.Join(tmpDir, "cache"))

	filename := "test-app-1.0.zip"

	if c.Exists(filename) {
		t.Errorf("expected file not to exist initially")
	}

	// Create a dummy temporary file to "downloaded"
	tmpDownload := filepath.Join(tmpDir, "tmp-download.zip")
	if err := os.WriteFile(tmpDownload, []byte("dummy content"), 0644); err != nil {
		t.Fatalf("failed to create dummy temp file: %v", err)
	}

	storedPath, err := c.Store(tmpDownload, filename)
	if err != nil {
		t.Fatalf("failed to store file: %v", err)
	}

	expectedPath := filepath.Join(c.DownloadsDir(), filename)
	if storedPath != expectedPath {
		t.Errorf("expected stored path %q, got %q", expectedPath, storedPath)
	}

	if !c.Exists(filename) {
		t.Errorf("expected file to exist after storing")
	}

	// Verify original temp file is gone
	if _, err := os.Stat(tmpDownload); !os.IsNotExist(err) {
		t.Errorf("expected original temp file to be removed")
	}
}

func TestCache_InvalidFilenames(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	c := New(filepath.Join(tmpDir, "cache"))

	invalidFilenames := []string{
		"../escape.zip",
		"sub/dir.zip",
		"/absolute/path.zip",
		"",
	}

	for _, fn := range invalidFilenames {
		t.Run(fn, func(t *testing.T) {
			if c.Exists(fn) {
				t.Errorf("Exists(%q) should be false for invalid filename", fn)
			}

			_, err := c.DownloadPath(fn)
			if err == nil {
				t.Errorf("DownloadPath(%q) should fail for invalid filename", fn)
			}

			_, err = c.Store("/tmp/some-file", fn)
			if err == nil {
				t.Errorf("Store(%q) should fail for invalid filename", fn)
			}
		})
	}
}

func TestCache_Store_MissingSource(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	c := New(filepath.Join(tmpDir, "cache"))

	_, err := c.Store(filepath.Join(tmpDir, "non-existent"), "file.zip")
	if err == nil {
		t.Error("Store should fail if source file is missing")
	}
}
