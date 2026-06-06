package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// FuzzWriteUpdateState verifies that the atomic state-file writer never panics,
// always produces valid JSON on disk, is idempotent (writing twice leaves a
// valid file), and that the resulting file always round-trips back to an
// UpdateState whose Phase field equals what was written.
//
// Run with:
//
//	go test ./pkg/installer/... -fuzz=FuzzWriteUpdateState -fuzztime=60s
func FuzzWriteUpdateState(f *testing.F) {
	// Seed: nominal staged state.
	f.Add("staged", "v1.0.0", "release", "/prefix/bin/grew", "/prefix/bin/grew.previous")
	// Seed: swapped state.
	f.Add("swapped", "v2.3.4", "patch", "/usr/local/bin/grew", "/usr/local/bin/grew.previous")
	// Seed: committed state.
	f.Add("committed", "v0.0.1", "source", "/home/user/.grew/bin/grew", "")
	// Seed: empty fields.
	f.Add("", "", "", "", "")
	// Seed: very long version string.
	f.Add("staged", string(make([]byte, 4096)), "release", "/bin/grew", "/bin/grew.previous")
	// Seed: path with special characters.
	f.Add("swapped", "v1.0.0", "release", "/path/with spaces/grew", "/path/with spaces/grew.previous")
	// Seed: unicode in method.
	f.Add("staged", "v1.0.0", "rele\u00e0se", "/bin/grew", "/bin/grew.previous")

	f.Fuzz(func(t *testing.T, phase, targetVer, method, currentPath, backupPath string) {
		dir := t.TempDir()
		sf := filepath.Join(dir, "update-state.json")

		state := &UpdateState{
			CurrentPath: currentPath,
			BackupPath:  backupPath,
			TargetVer:   targetVer,
			Method:      method,
			Phase:       updatePhase(phase),
			UpdatedAt:   time.Now().UTC(),
		}

		// Invariant 1: writeUpdateState must never panic.
		err := writeUpdateState(sf, state)
		if err != nil {
			// An error (e.g. permission denied) is acceptable — it must not panic.
			return
		}

		// Invariant 2: the file must exist after a successful write.
		data, readErr := os.ReadFile(sf)
		if readErr != nil {
			t.Fatalf("state file missing after successful writeUpdateState: %v", readErr)
		}

		// Invariant 3: the file must contain valid JSON.
		var got UpdateState
		if jsonErr := json.Unmarshal(data, &got); jsonErr != nil {
			t.Fatalf("state file is not valid JSON: %v (content: %q)", jsonErr, data)
		}

		// Invariant 4: Phase round-trips exactly.
		if got.Phase != updatePhase(phase) {
			t.Fatalf("Phase mismatch: wrote %q, read back %q", phase, got.Phase)
		}

		// Invariant 5: writing the same state again must leave the file valid
		// (idempotency — tests the atomic rename path is re-entrant).
		if err2 := writeUpdateState(sf, state); err2 != nil {
			return // OS failure on second write is acceptable
		}
		data2, readErr2 := os.ReadFile(sf)
		if readErr2 != nil {
			t.Fatalf("state file missing after second writeUpdateState: %v", readErr2)
		}
		var got2 UpdateState
		if jsonErr := json.Unmarshal(data2, &got2); jsonErr != nil {
			t.Fatalf("state file not valid JSON after second write: %v (content: %q)", jsonErr, data2)
		}
		if got2.Phase != updatePhase(phase) {
			t.Fatalf("Phase mismatch after second write: wrote %q, read back %q", phase, got2.Phase)
		}
	})
}
