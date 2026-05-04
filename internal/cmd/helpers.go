package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/receipt"
)

func FilterByManifest(packages []cellar.InstalledPackage, cel *cellar.Cellar, onRequest, asDep, builtSrc, pouredBottle bool) []cellar.InstalledPackage {
	var filtered []cellar.InstalledPackage
	for _, p := range packages {
		kegPath, err := cel.KegPath(p.Name, p.Version)
		if err != nil {
			continue
		}
		rcpt, err := receipt.Load(kegPath)
		if err != nil {
			continue
		}

		if onRequest && !rcpt.InstalledOnRequest {
			continue
		}
		if asDep && rcpt.InstalledOnRequest {
			continue
		}
		if builtSrc && !rcpt.BuiltFromSource {
			continue
		}
		if pouredBottle && rcpt.BuiltFromSource {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func JoinVersions(vers []string) string {
	if len(vers) == 0 {
		return ""
	}
	if len(vers) == 1 {
		return vers[0]
	}
	return strings.Join(vers, ", ")
}

func RemoveIfWithinAllowed(tmpDir, cacheDir, candidate string) error {
	if tmpDir == "" || cacheDir == "" || candidate == "" {
		return nil
	}
	cleanTmp, err := filepath.Abs(filepath.Clean(tmpDir))
	if err != nil {
		return err
	}
	cleanCache, err := filepath.Abs(filepath.Clean(cacheDir))
	if err != nil {
		return err
	}
	cleanCandidate, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return err
	}

	if strings.HasPrefix(cleanCandidate, cleanTmp) || strings.HasPrefix(cleanCandidate, cleanCache) {
		return os.Remove(cleanCandidate)
	}
	return fmt.Errorf("refusing to remove file outside of allowed directories: %s", cleanCandidate)
}

func URLExt(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := filepath.Base(u.Path)
	if idx := strings.Index(base, ".tar."); idx != -1 {
		return base[idx:]
	}
	return filepath.Ext(base)
}
