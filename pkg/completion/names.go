package completion

import (
	"encoding/json"
	"os"
	"time"

	"github.com/homegrew/grew/pkg/context"
	"github.com/homegrew/grew/pkg/homebrew"
	"github.com/homegrew/grew/pkg/safepath"
)

const cacheTTL = 24 * time.Hour

// NamesCache caches formula and cask name lists fetched from the Homebrew API.
type NamesCache struct {
	dir string
}

// New returns a NamesCache that stores files under the user's OS cache directory.
// If the cache directory cannot be safely resolved, caching is disabled and
// completion falls back to live fetches.
func New(cacheDir string) *NamesCache {
	if cacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return &NamesCache{}
		}

		cacheBase, err := safepath.SafeJoin(base, "Homegrew")
		if err != nil || safepath.SafeAbsolutePath(cacheBase) != nil {
			return &NamesCache{}
		}

		cacheDir, err = safepath.SafeJoin(cacheBase, "completion")
		if err != nil {
			return &NamesCache{}
		}
	}

	return &NamesCache{dir: cacheDir}
}

func NewWithContent(ctx *context.Context) *NamesCache {
	// dir := filepath.Join(ctx.Paths.Cache, "completion")
	// 		if err := os.MkdirAll(dir, 0755); err != nil {
	// 			return &NamesCache{}
	// }

	return New(ctx.Paths.Cache)
}

type namesCacheFile struct {
	Names     []string  `json:"names"`
	FetchedAt time.Time `json:"fetched_at"`
}

// FormulaNames returns all available formula names, using the cache when fresh.
func (nc *NamesCache) FormulaNames() ([]string, error) {
	return nc.load("formula-names.json", homebrew.FetchFormulaNames)
}

// CaskNames returns all available cask tokens, using the cache when fresh.
func (nc *NamesCache) CaskNames() ([]string, error) {
	return nc.load("cask-names.json", homebrew.FetchCaskNames)
}

func (nc *NamesCache) load(filename string, fetch func() ([]string, error)) ([]string, error) {
	if nc.dir == "" || safepath.SafeAbsolutePath(nc.dir) != nil {
		return fetch()
	}

	path, err := safepath.SafeJoin(nc.dir, filename)
	if err != nil {
		return fetch()
	}

	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < cacheTTL {
		if data, err := os.ReadFile(path); err == nil {
			var cf namesCacheFile
			if json.Unmarshal(data, &cf) == nil && len(cf.Names) > 0 {
				return cf.Names, nil
			}
		}
	}

	names, err := fetch()
	if err != nil {
		return nil, err
	}

	// Best-effort write; completion degrades gracefully on failure.
	if mkErr := os.MkdirAll(nc.dir, 0755); mkErr == nil {
		if data, jsonErr := json.Marshal(namesCacheFile{Names: names, FetchedAt: time.Now()}); jsonErr == nil {
			_ = os.WriteFile(path, data, 0644)
		}
	}

	return names, nil
}
