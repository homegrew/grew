package cache

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/pkg/safepath"
)

// downloadsSubdir is the cache subdirectory holding downloaded artifacts.
const downloadsSubdir = "downloads"

// Cache manages the local file cache for grew.
type Cache struct {
	dir string
	fs  fs.FS
}

// New creates a new Cache rooted at dir.
func New(dir string) *Cache {
	cleanDir := filepath.Clean(dir)
	if err := safepath.SafeAbsolutePath(cleanDir); err != nil {
		return &Cache{
			dir: "",
			fs:  nil,
		}
	}
	return &Cache{
		dir: cleanDir,
		fs:  os.DirFS(cleanDir),
	}
}

// Dir returns the root directory of the cache.
func (c *Cache) Dir() string {
	return c.dir
}

// DownloadsDir returns the absolute path to the directory used for caching downloads.
func (c *Cache) DownloadsDir() string {
	if c.dir == "" {
		return c.dir
	}
	return filepath.Join(c.dir, downloadsSubdir)
}

// DownloadPath returns the safe, absolute path for a cached download.
func (c *Cache) DownloadPath(filename string) (string, error) {
	if c.dir == "" {
		return "", fmt.Errorf("cache directory not set")
	}
	if err := safepath.SafePathComponent(filename); err != nil {
		return "", fmt.Errorf("invalid download filename: %w", err)
	}
	return safepath.SafeJoin(c.dir, downloadsSubdir, filename)
}

// Exists reports whether the given filename exists in the download cache.
func (c *Cache) Exists(filename string) bool {
	if c.fs == nil {
		return false
	}
	if err := safepath.SafePathComponent(filename); err != nil {
		return false
	}
	// os.DirFS requires relative paths without the root prefix.
	relPath := filepath.Join(downloadsSubdir, filename)
	_, err := fs.Stat(c.fs, relPath)
	return err == nil
}

// Store moves a temporary file into the download cache and returns the new cache path.
func (c *Cache) Store(tmpPath, filename string) (string, error) {
	cachePath, err := c.DownloadPath(filename)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return "", fmt.Errorf("create cache directory: %w", err)
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		return "", fmt.Errorf("move to cache: %w", err)
	}
	return cachePath, nil
}
