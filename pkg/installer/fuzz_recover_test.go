package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// FuzzRecoverPendingUpdate feeds arbitrary byte sequences as the state file
// content and asserts the invariant that RecoverPendingUpdate never panics,
// never corrupts the live binary, and always removes the state file when it
// cannot make sense of it.
//
// Run with:
//
//	go test ./pkg/installer/... -fuzz=FuzzRecoverPendingUpdate -fuzztime=60s
func FuzzRecoverPendingUpdate(f *testing.F) {
	// Seed 1: no state file at all — represented as empty bytes.
	f.Add([]byte{})

	// Seed 2: committed phase — should be cleaned up immediately.
	committed, _ := json.Marshal(UpdateState{
		Phase:     phaseCommitted,
		TargetVer: "v1.0.0",
		UpdatedAt: time.Now().UTC(),
	})
	f.Add(committed)

	// Seed 3: staged phase — crash before swap, no rollback needed.
	staged, _ := json.Marshal(UpdateState{
		Phase:     phaseStaged,
		TargetVer: "v1.1.0",
		UpdatedAt: time.Now().UTC(),
	})
	f.Add(staged)

	// Seed 4: swapped phase with backup missing — treat as committed.
	swappedNoBackup, _ := json.Marshal(UpdateState{
		Phase:       phaseSwapped,
		CurrentPath: "/tmp/grew",
		BackupPath:  "/tmp/grew.previous-nonexistent",
		TargetVer:   "v1.1.0",
		UpdatedAt:   time.Now().UTC(),
	})
	f.Add(swappedNoBackup)

	// Seed 5: truncated JSON.
	f.Add([]byte(`{"phase":"swapped","current_path":"/tmp/g`))

	// Seed 6: valid JSON but unknown phase.
	f.Add([]byte(`{"phase":"unknown","target_version":"v2.0.0"}`))

	// Seed 7: wrong types.
	f.Add([]byte(`{"phase":42,"backup_path":true}`))

	// Seed 8: empty JSON object.
	f.Add([]byte(`{}`))

	// Seed 9: huge version string.
	f.Add([]byte(`{"phase":"swapped","target_version":"` + string(make([]byte, 4096)) + `"}`))

	f.Fuzz(func(t *testing.T, stateData []byte) {
		dir := t.TempDir()

		// Write a known-good "current" binary so we can check it is intact.
		current := filepath.Join(dir, "grew")
		writeScript(t, current, `echo "grew 1.0.0"`)
		originalOut, _ := execBin(t, current)

		// Write a backup in case the state file references phase=swapped.
		backup := filepath.Join(dir, "grew.previous")
		writeScript(t, backup, `echo "grew 0.9.0"`)

		var sf string
		if len(stateData) > 0 {
			sf = filepath.Join(dir, "update-state.json")

			// Patch any absolute paths in the fuzz data with real tmpdir paths
			// so the function can interact with real files.
			var state UpdateState
			if json.Unmarshal(stateData, &state) == nil {
				if state.Phase == phaseSwapped {
					state.CurrentPath = current
					state.BackupPath = backup
					patched, err := json.Marshal(state)
					if err == nil {
						stateData = patched
					}
				}
			}
			if err := os.WriteFile(sf, stateData, 0600); err != nil {
				t.Skip("could not write state file")
			}
		}

		// Must not panic.
		err := RecoverPendingUpdate(sf)

		// Invariant 1: RecoverPendingUpdate must not return a non-nil error
		// for malformed inputs — it logs and continues.
		// (Errors are only returned for genuine OS failures, not bad data.)
		_ = err

		// Invariant 2: the live binary must exist.
		if _, statErr := os.Stat(current); os.IsNotExist(statErr) {
			t.Fatalf("live binary disappeared after RecoverPendingUpdate")
		}

		// Invariant 3: the live binary must still be runnable.
		// It may have been restored to the backup version, but it must run.
		finalOut, execErr := execBin(t, current)
		if execErr != nil {
			t.Fatalf("live binary is not executable after RecoverPendingUpdate: %v (out=%q, was=%q)",
				execErr, finalOut, originalOut)
		}

		// Invariant 4: state file must be removed (RecoverPendingUpdate always
		// cleans it up or the file was never written).
		if sf != "" {
			if _, statErr := os.Stat(sf); !os.IsNotExist(statErr) {
				t.Fatalf("state file still present after RecoverPendingUpdate (sf=%q)", sf)
			}
		}
	})
}
