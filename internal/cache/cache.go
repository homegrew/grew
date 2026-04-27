package cache

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/pkg/safepath"
)

// Cache manages the local file cache for grew.
type Cache struct {
	Dir string
}

// New creates a new Cache rooted at dir.
func New(dir string) *Cache {
	return &Cache{Dir: dir}
}

// DownloadsDir returns the directory used for caching downloads.
func (c *Cache) DownloadsDir() string {
	return filepath.Join(c.Dir, "downloads")
}

// DownloadPath returns the safe, canonical path for a cached download.
// It ensures the resulting path is strictly within the downloads directory.
func (c *Cache) DownloadPath(filename string) (string, error) {
	if c.Dir == "" {
		return "", fmt.Errorf("cache directory not set")
	}
	return safepath.SafeJoin(c.DownloadsDir(), filename)
}

// Exists reports whether the given filename exists in the download cache.
func (c *Cache) Exists(filename string) bool {
	path, err := c.DownloadPath(filename)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
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
