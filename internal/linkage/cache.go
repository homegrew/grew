package linkage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const cacheFile = ".LINKAGE.json"

// CachePath returns the path to the linkage cache file for a keg.
func CachePath(kegPath string) string {
	return filepath.Join(kegPath, cacheFile)
}

// SaveCache writes a Result to the cache file in the keg directory.
func SaveCache(r *Result) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal linkage cache: %w", err)
	}
	path := CachePath(r.KegPath)
	// Write atomically: temp file + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write linkage cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename linkage cache: %w", err)
	}
	return nil
}

// LoadCache reads a cached Result from the keg directory.
// Returns nil, nil if no cache exists.
func LoadCache(kegPath string) (*Result, error) {
	data, err := os.ReadFile(CachePath(kegPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read linkage cache: %w", err)
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse linkage cache: %w", err)
	}
	return &r, nil
}
