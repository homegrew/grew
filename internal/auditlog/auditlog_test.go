package auditlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLog_CreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l := New(dir)

	l.Log(ActionInstall, "jq", "1.7.1", "abc123", "bottle")

	path := filepath.Join(dir, logFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"action":"install"`) {
		t.Errorf("expected action install in log, got: %s", s)
	}
	if !strings.Contains(s, `"name":"jq"`) {
		t.Errorf("expected name jq in log, got: %s", s)
	}
	if !strings.Contains(s, `"version":"1.7.1"`) {
		t.Errorf("expected version in log, got: %s", s)
	}
	if !strings.Contains(s, `"sha256":"abc123"`) {
		t.Errorf("expected sha256 in log, got: %s", s)
	}
}

func TestLog_AppendsMultipleEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l := New(dir)

	l.Log(ActionInstall, "jq", "1.7.1", "", "")
	l.Log(ActionUninstall, "jq", "1.7.1", "", "")
	l.Log(ActionInstall, "curl", "8.0", "", "")

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Action != ActionInstall || entries[0].Name != "jq" {
		t.Errorf("entry 0: got %+v", entries[0])
	}
	if entries[1].Action != ActionUninstall {
		t.Errorf("entry 1: got %+v", entries[1])
	}
	if entries[2].Name != "curl" {
		t.Errorf("entry 2: got %+v", entries[2])
	}
}

func TestLog_HasTimestamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l := New(dir)
	l.Log(ActionPin, "jq", "", "", "")

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	// Should be RFC3339 format.
	if !strings.Contains(entries[0].Timestamp, "T") {
		t.Errorf("timestamp not RFC3339: %s", entries[0].Timestamp)
	}
}

func TestLog_HasUser(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l := New(dir)
	l.Log(ActionInstall, "jq", "1.0", "", "")

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].User == "" {
		t.Error("expected non-empty user")
	}
}

func TestRead_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for missing log, got %d", len(entries))
	}
}

func TestRead_MalformedLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)
	// Write a mix of valid and invalid JSON lines.
	content := `{"action":"install","name":"jq","version":"1.0","timestamp":"2026-01-01T00:00:00Z"}
not json
{"action":"uninstall","name":"curl","version":"2.0","timestamp":"2026-01-02T00:00:00Z"}
`
	os.WriteFile(path, []byte(content), 0644)

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Should have parsed 2 valid entries, skipping the bad line.
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestLog_AllActions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l := New(dir)

	actions := []Action{
		ActionInstall, ActionUninstall, ActionReinstall,
		ActionUpgrade, ActionSelfUpdate, ActionPin, ActionUnpin,
		ActionLink, ActionUnlink, ActionCaskInstall, ActionCaskRemove,
	}
	for _, a := range actions {
		l.Log(a, "test", "1.0", "", "")
	}

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(entries) != len(actions) {
		t.Fatalf("expected %d entries, got %d", len(actions), len(entries))
	}
	for i, e := range entries {
		if e.Action != actions[i] {
			t.Errorf("entry %d: got action %q, want %q", i, e.Action, actions[i])
		}
	}
}

func TestLog_Detail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l := New(dir)
	l.Log(ActionUpgrade, "jq", "1.8", "", "1.7.1 -> 1.8")

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if entries[0].Detail != "1.7.1 -> 1.8" {
		t.Errorf("detail = %q, want %q", entries[0].Detail, "1.7.1 -> 1.8")
	}
}

func TestLog_NonexistentDir(t *testing.T) {
	t.Parallel()
	// Logging to a nonexistent directory should not panic.
	l := New("/nonexistent/path/that/does/not/exist")
	l.Log(ActionInstall, "jq", "1.0", "", "") // should silently fail
}
