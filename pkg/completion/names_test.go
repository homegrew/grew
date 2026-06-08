package completion

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNamesCache_FreshCacheHit(t *testing.T) {
	dir := t.TempDir()
	nc := New(dir)

	calls := 0
	stub := func() ([]string, error) {
		calls++
		return []string{"a", "b"}, nil
	}

	// Prime the cache by calling load once.
	if _, err := nc.load("test.json", stub); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 fetch call, got %d", calls)
	}

	// Second call should hit the cache.
	names, err := nc.load("test.json", stub)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected still 1 fetch call (cache hit), got %d", calls)
	}
	if len(names) != 2 || names[0] != "a" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestNamesCache_StaleCacheFetch(t *testing.T) {
	dir := t.TempDir()
	nc := New(dir)

	// Write a cache file with an old mtime.
	cacheDir := filepath.Join(dir, "completion")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cacheDir, "stale.json")
	old := namesCacheFile{Names: []string{"old"}, FetchedAt: time.Now().Add(-48 * time.Hour)}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	// Set mtime to 48 hours ago.
	pastTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}

	calls := 0
	stub := func() ([]string, error) {
		calls++
		return []string{"fresh"}, nil
	}

	names, err := nc.load("stale.json", stub)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected fetch on stale cache, got %d calls", calls)
	}
	if len(names) != 1 || names[0] != "fresh" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestNamesCache_CorruptCacheFetch(t *testing.T) {
	dir := t.TempDir()
	nc := New(dir)

	// Write a corrupt (non-JSON) cache file with a fresh mtime.
	cacheDir := filepath.Join(dir, "completion")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cacheDir, "corrupt.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	calls := 0
	stub := func() ([]string, error) {
		calls++
		return []string{"ok"}, nil
	}

	names, err := nc.load("corrupt.json", stub)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected fetch on corrupt cache, got %d calls", calls)
	}
	if len(names) != 1 || names[0] != "ok" {
		t.Fatalf("unexpected names: %v", names)
	}
}
