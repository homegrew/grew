package lockfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/snapshot"
)

// LockFileName is the name of the lockfile stored at the grew root.
const LockFileName = "grew.lock"

// LockFile records the exact state of all installed formulas.
type LockFile struct {
	Version int              `json:"version"` // schema version, currently 1
	Entries map[string]Entry `json:"entries"`  // keyed by formula name
}

// Entry records one installed formula.
type Entry struct {
	Version      string   `json:"version"`
	SHA256       string   `json:"sha256"`                    // download hash
	DownloadURL  string   `json:"download_url"`
	Platform     string   `json:"platform"`
	Dependencies []string `json:"dependencies,omitempty"`
	KegSHA256    string   `json:"keg_sha256,omitempty"` // from snapshot manifest if available
}

// Discrepancy describes one difference between the lockfile and installed state.
type Discrepancy struct {
	Name   string // formula name
	Kind   string // "missing", "extra", "version_mismatch", "hash_mismatch"
	Detail string // human-readable description
}

// LockFilePath returns the path to the lockfile for the given grew root.
func LockFilePath(grewRoot string) string {
	return filepath.Join(grewRoot, LockFileName)
}

// Load reads and parses the lockfile. Returns an empty LockFile (not an error)
// if the file does not exist.
func Load(grewRoot string) (*LockFile, error) {
	path := LockFilePath(grewRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LockFile{Version: 1, Entries: make(map[string]Entry)}, nil
		}
		return nil, fmt.Errorf("read lockfile: %w", err)
	}

	var lf LockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parse lockfile: %w", err)
	}
	if lf.Entries == nil {
		lf.Entries = make(map[string]Entry)
	}
	return &lf, nil
}

// Save atomically writes the lockfile with sorted keys and indented JSON.
func Save(lf *LockFile, grewRoot string) error {
	data, err := marshalSorted(lf)
	if err != nil {
		return fmt.Errorf("marshal lockfile: %w", err)
	}

	dest := LockFilePath(grewRoot)
	dir := filepath.Dir(dest)

	tmp, err := os.CreateTemp(dir, ".grew-lock-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp lockfile: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp lockfile: %w", err)
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dest)
}

// marshalSorted produces JSON with entries sorted by name for deterministic output.
func marshalSorted(lf *LockFile) ([]byte, error) {
	// Build an ordered representation.
	type orderedLockFile struct {
		Version int                    `json:"version"`
		Entries json.RawMessage        `json:"entries"`
	}

	// Sort keys.
	names := make([]string, 0, len(lf.Entries))
	for name := range lf.Entries {
		names = append(names, name)
	}
	sort.Strings(names)

	// Build ordered entries as a JSON object manually via encoder.
	// We use an ordered slice of key-value pairs encoded sequentially.
	buf := []byte{'{'}
	for i, name := range names {
		if i > 0 {
			buf = append(buf, ',')
		}
		key, _ := json.Marshal(name)
		val, err := json.Marshal(lf.Entries[name])
		if err != nil {
			return nil, err
		}
		buf = append(buf, key...)
		buf = append(buf, ':')
		buf = append(buf, val...)
	}
	buf = append(buf, '}')

	out := orderedLockFile{
		Version: lf.Version,
		Entries: json.RawMessage(buf),
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Generate walks the cellar and builds a complete lockfile from the currently
// installed state. Snapshot manifests are read where available.
func Generate(grewRoot string, cellarPath string) (*LockFile, error) {
	cel := &cellar.Cellar{Path: cellarPath}
	pkgs, err := cel.List()
	if err != nil {
		return nil, fmt.Errorf("list cellar: %w", err)
	}

	platform := formula.PlatformKey()
	lf := &LockFile{
		Version: 1,
		Entries: make(map[string]Entry),
	}

	for _, pkg := range pkgs {
		entry := Entry{
			Version:  pkg.Version,
			Platform: platform,
		}

		kegPath := cel.KegPath(pkg.Name, pkg.Version)
		if snapshot.Exists(kegPath) {
			m, err := snapshot.Load(kegPath)
			if err == nil {
				entry.SHA256 = m.DownloadSHA256
				entry.DownloadURL = m.DownloadURL
				entry.Platform = m.Platform
				entry.Dependencies = m.Dependencies
				entry.KegSHA256 = m.KegSHA256
			}
		}

		lf.Entries[pkg.Name] = entry
	}

	return lf, nil
}

// Check compares the lockfile against the currently installed packages and
// returns any discrepancies found.
func Check(lf *LockFile, cellarPath string) ([]Discrepancy, error) {
	cel := &cellar.Cellar{Path: cellarPath}
	pkgs, err := cel.List()
	if err != nil {
		return nil, fmt.Errorf("list cellar: %w", err)
	}

	// Build a map of installed packages.
	installed := make(map[string]cellar.InstalledPackage)
	for _, pkg := range pkgs {
		installed[pkg.Name] = pkg
	}

	var discrepancies []Discrepancy

	// Check for entries in lock but not installed ("missing"), or version/hash mismatches.
	lockNames := make([]string, 0, len(lf.Entries))
	for name := range lf.Entries {
		lockNames = append(lockNames, name)
	}
	sort.Strings(lockNames)

	for _, name := range lockNames {
		entry := lf.Entries[name]
		pkg, ok := installed[name]
		if !ok {
			discrepancies = append(discrepancies, Discrepancy{
				Name:   name,
				Kind:   "missing",
				Detail: fmt.Sprintf("locked at %s but not installed", entry.Version),
			})
			continue
		}

		if pkg.Version != entry.Version {
			discrepancies = append(discrepancies, Discrepancy{
				Name:   name,
				Kind:   "version_mismatch",
				Detail: fmt.Sprintf("locked at %s but installed %s", entry.Version, pkg.Version),
			})
			continue
		}

		// Check keg hash if both sides have one.
		if entry.KegSHA256 != "" {
			kegPath := cel.KegPath(name, pkg.Version)
			if snapshot.Exists(kegPath) {
				m, err := snapshot.Load(kegPath)
				if err == nil && m.KegSHA256 != entry.KegSHA256 {
					discrepancies = append(discrepancies, Discrepancy{
						Name:   name,
						Kind:   "hash_mismatch",
						Detail: fmt.Sprintf("keg hash %s != locked %s", m.KegSHA256, entry.KegSHA256),
					})
				}
			}
		}
	}

	// Check for installed packages not in the lockfile ("extra").
	installedNames := make([]string, 0, len(installed))
	for name := range installed {
		installedNames = append(installedNames, name)
	}
	sort.Strings(installedNames)

	for _, name := range installedNames {
		if _, ok := lf.Entries[name]; !ok {
			discrepancies = append(discrepancies, Discrepancy{
				Name:   name,
				Kind:   "extra",
				Detail: fmt.Sprintf("installed (%s) but not in lockfile", installed[name].Version),
			})
		}
	}

	return discrepancies, nil
}
