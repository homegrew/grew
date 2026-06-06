package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// FuzzTransactionalInstall drives the full backup→swap→verify→commit/rollback
// state machine with randomized binary behaviour and health check outcomes.
//
// The fuzz corpus encodes the scenario as four boolean bytes:
//
//	b[0] > 0  → current binary exists      (false = simulate missing install)
//	b[1] > 0  → staged binary exists       (false = staged was never written)
//	b[2] > 0  → health check passes        (false = post-swap rejection)
//	b[3] > 0  → use state file on disk     (false = empty stateFile path)
//
// After every run the following invariants are enforced:
//
//  1. The live binary path either holds the original OR the new content — never
//     a partial / missing binary.
//  2. A failed health check MUST leave the original binary in place.
//  3. A successful commit MUST remove the backup file.
//  4. The state file MUST be absent after the call returns.
//  5. If the staged binary was never written, the original must be untouched.
//
// Run with:
//
//	go test ./pkg/installer/... -fuzz=FuzzTransactionalInstall -fuzztime=120s
func FuzzTransactionalInstall(f *testing.F) {
	// Seed the four canonical scenarios as compact byte slices.

	// Happy path: all present, health check passes.
	f.Add([]byte{1, 1, 1, 1})
	// Staged missing: can't even start the swap.
	f.Add([]byte{1, 0, 1, 1})
	// Health check fails: rollback must fire.
	f.Add([]byte{1, 1, 0, 1})
	// No state file: crash recovery disabled, otherwise same as happy path.
	f.Add([]byte{1, 1, 1, 0})
	// Health check fails, no state file.
	f.Add([]byte{1, 1, 0, 0})
	// Current binary missing: edge case, TransactionalInstall should still be
	// safe (backup rename will fail gracefully).
	f.Add([]byte{0, 1, 1, 1})
	// Nothing present.
	f.Add([]byte{0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, flags []byte) {
		if len(flags) < 4 {
			t.Skip()
		}
		currentExists := flags[0] > 0
		stagedExists := flags[1] > 0
		healthPass := flags[2] > 0
		useStateFile := flags[3] > 0

		dir := t.TempDir()
		current := filepath.Join(dir, "grew")
		staged := filepath.Join(dir, ".grew-staged")
		backup := filepath.Join(dir, "grew.previous")

		const originalContent = `echo "grew 1.0.0"`
		const newContent = `echo "grew 1.1.0"`

		if currentExists {
			writeScript(t, current, originalContent)
		}
		if stagedExists {
			writeScript(t, staged, newContent)
		}

		// Build a health checker whose result is controlled by the fuzz corpus.
		var checks []HealthChecker
		if healthPass {
			checks = []HealthChecker{VersionHealthChecker{Expected: "1.1.0"}}
		} else {
			checks = []HealthChecker{&callRecorder{fn: func(_ context.Context, _ string) error {
				return fmt.Errorf("fuzz-injected health check failure")
			}}}
		}

		var sf string
		if useStateFile {
			sf = filepath.Join(dir, "update-state.json")
		}

		// Must not panic under any combination of inputs.
		err := TransactionalInstall(current, staged, backup, sf, "v1.1.0", "release", checks)

		// ----------------------------------------------------------------
		// Invariant 1: if the current binary existed before the call it must
		// still exist after — either as the committed new binary or as the
		// restored original. When currentExists=false the backup rename fails
		// before any swap occurs, so there is nothing to assert.
		if currentExists {
			if _, statErr := os.Stat(current); os.IsNotExist(statErr) {
				t.Fatalf("live binary disappeared after TransactionalInstall (err=%v)", err)
			}
		}

		// ----------------------------------------------------------------
		// Invariant 2: if the health check was set to fail AND the staged binary
		// existed AND the current binary existed, the rollback must have restored
		// the original binary content.
		if !healthPass && currentExists && stagedExists {
			got, execErr := execBin(t, current)
			if execErr != nil {
				t.Fatalf("current binary not executable after health-check failure + rollback: %v", execErr)
			}
			if got != "grew 1.0.0" {
				t.Fatalf("rollback failed: current binary outputs %q, want %q", got, "grew 1.0.0")
			}
		}

		// ----------------------------------------------------------------
		// Invariant 3: if the call succeeded (nil error), the backup must be
		// gone and the new binary must be live.
		if err == nil {
			if _, statErr := os.Stat(backup); !os.IsNotExist(statErr) {
				t.Fatalf("backup file still present after successful commit")
			}
			if currentExists && stagedExists {
				got, execErr := execBin(t, current)
				if execErr != nil {
					t.Fatalf("current binary not executable after successful commit: %v", execErr)
				}
				if got != "grew 1.1.0" {
					t.Fatalf("commit did not activate new binary: got %q", got)
				}
			}
		}

		// ----------------------------------------------------------------
		// Invariant 4: state file must be absent after the call returns,
		// whether it succeeded or not.
		if sf != "" {
			if _, statErr := os.Stat(sf); !os.IsNotExist(statErr) {
				t.Fatalf("state file still present after TransactionalInstall returned")
			}
		}

		// ----------------------------------------------------------------
		// Invariant 5: if the staged binary never existed, the original must be
		// untouched (backup rename will fail before any swap occurs — if current
		// existed it should still be present and contain original content).
		if !stagedExists && currentExists {
			got, execErr := execBin(t, current)
			if execErr != nil {
				t.Fatalf("current binary not executable when staged was missing: %v", execErr)
			}
			if got != "grew 1.0.0" {
				t.Fatalf("current binary mutated when staged was missing: got %q", got)
			}
		}
	})
}
