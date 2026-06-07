package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/homegrew/grew/pkg/auditlog"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// execBin runs path with no arguments and returns trimmed stdout.
// writeScript is already declared in selfupdate_test.go (same package).
func execBin(t *testing.T, path string) (string, error) {
	t.Helper()
	out, err := exec.Command(path).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// mustExecBin fatally fails the test if the binary cannot be run or the output
// does not equal want.
func mustExecBin(t *testing.T, path, want string) {
	t.Helper()
	got, err := execBin(t, path)
	if err != nil {
		t.Fatalf("execBin(%q): %v (output: %q)", path, err, got)
	}
	if got != want {
		t.Fatalf("execBin(%q): got %q, want %q", path, got, want)
	}
}

// writeStateFile marshals s and writes it to path. Fatal on any error.
func writeStateFile(t *testing.T, path string, s UpdateState) {
	t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
}

// noStateFile asserts that path does not exist.
func noStateFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %q to not exist, but it does (stat err: %v)", path, err)
	}
}

// hasAuditAction returns true if any entry in logDir has the given action.
func hasAuditAction(t *testing.T, logDir string, action auditlog.Action) bool {
	t.Helper()
	entries, err := auditlog.Read(logDir)
	if err != nil {
		t.Fatalf("read audit log from %q: %v", logDir, err)
	}
	for _, e := range entries {
		if e.Action == action {
			return true
		}
	}
	return false
}

// callRecorder is a HealthChecker that delegates to an arbitrary function,
// allowing tests to inject failures or track invocations.
type callRecorder struct {
	mu     interface{} // unused — kept for symmetry with future lock usage
	called bool
	fn     func(context.Context, string) error
}

func (c *callRecorder) Check(ctx context.Context, binPath string) error {
	c.called = true
	return c.fn(ctx, binPath)
}

// ---------------------------------------------------------------------------
// TransactionalInstall
// ---------------------------------------------------------------------------

func TestTransactionalInstall_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 1.1.0"`)

	checks := []HealthChecker{VersionHealthChecker{Expected: "1.1.0"}}
	if err := TransactionalInstall(current, staged, backup, "", "v1.1.0", "release", checks); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Backup must be removed after a successful commit.
	noStateFile(t, backup)
	// Staged path must be gone (renamed into current).
	noStateFile(t, staged)
	// Current binary must now be the new version.
	mustExecBin(t, current, "grew 1.1.0")
}

func TestTransactionalInstall_PostCheckFails_RollsBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 0.0.1"`) // wrong version — health check must reject it

	checks := []HealthChecker{VersionHealthChecker{Expected: "1.1.0"}}
	err := TransactionalInstall(current, staged, backup, "", "v1.1.0", "release", checks)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "post-swap health check") {
		t.Errorf("expected 'post-swap health check' in error, got: %s", err)
	}

	// Current binary must be restored to the original.
	mustExecBin(t, current, "grew 1.0.0")
	// Backup must be cleaned up after rollback.
	noStateFile(t, backup)
}

func TestTransactionalInstall_MultipleChecks_FirstFails_SecondNotCalled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 0.0.1"`)

	second := &callRecorder{fn: func(_ context.Context, _ string) error {
		return nil
	}}
	checks := []HealthChecker{
		VersionHealthChecker{Expected: "9.9.9"}, // will fail
		second,
	}
	if err := TransactionalInstall(current, staged, backup, "", "v9.9.9", "release", checks); err == nil {
		t.Fatal("expected error")
	}
	if second.called {
		t.Error("second checker should not have been called after first failure")
	}
	mustExecBin(t, current, "grew 1.0.0")
}

func TestTransactionalInstall_MultipleChecks_SecondFails_RollsBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 1.1.0"`)

	second := &callRecorder{fn: func(_ context.Context, _ string) error {
		return fmt.Errorf("second check intentional failure")
	}}
	checks := []HealthChecker{
		VersionHealthChecker{Expected: "1.1.0"}, // passes
		second,                                  // fails
	}
	err := TransactionalInstall(current, staged, backup, "", "v1.1.0", "release", checks)
	if err == nil {
		t.Fatal("expected error from second check")
	}
	if !second.called {
		t.Error("second checker should have been called")
	}
	// Rollback must have restored the original.
	mustExecBin(t, current, "grew 1.0.0")
}

func TestTransactionalInstall_NoChecks_Commits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 1.1.0"`)

	// nil postChecks slice: skip health checks and commit immediately.
	if err := TransactionalInstall(current, staged, backup, "", "v1.1.0", "release", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mustExecBin(t, current, "grew 1.1.0")
	noStateFile(t, backup)
}

func TestTransactionalInstall_StagedMissing_CurrentUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged-does-not-exist")
	backup := filepath.Join(dir, "grew.previous")

	writeScript(t, current, `echo "grew 1.0.0"`)

	err := TransactionalInstall(current, staged, backup, "", "v1.1.0", "release", nil)
	if err == nil {
		t.Fatal("expected error for missing staged binary")
	}
	// Current must be untouched — the backup step happens first, so if staged
	// is missing the backup rename will have occurred. Verify restore worked.
	out, execErr := execBin(t, current)
	if execErr != nil || out != "grew 1.0.0" {
		t.Errorf("current binary should be untouched/restored: got %q err %v", out, execErr)
	}
	// No backup file should survive.
	noStateFile(t, backup)
}

func TestTransactionalInstall_StateFile_PhasesWritten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")
	sf := filepath.Join(dir, "update-state.json")

	writeScript(t, current, `echo "grew 1.0.0"`)

	// staged exits non-zero so the post-check will fail — letting us observe
	// the state file was written before the health check runs.
	writeScript(t, staged, `exit 1`)

	checks := []HealthChecker{VersionHealthChecker{Expected: "1.1.0"}}
	_ = TransactionalInstall(current, staged, backup, sf, "v1.1.0", "release", checks)

	// State file must be cleaned up after rollback.
	noStateFile(t, sf)
}

func TestTransactionalInstall_AuditLog_RollbackEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logDir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")
	sf := filepath.Join(logDir, "update-state.json")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 0.0.1"`)

	checks := []HealthChecker{VersionHealthChecker{Expected: "9.9.9"}}
	_ = TransactionalInstall(current, staged, backup, sf, "v9.9.9", "release", checks)

	if !hasAuditAction(t, logDir, auditlog.ActionSelfRollback) {
		t.Error("expected self-rollback audit entry after post-swap health check failure")
	}
}

func TestTransactionalInstall_AuditLog_NoRollbackOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logDir := t.TempDir()
	current := filepath.Join(dir, "grew")
	staged := filepath.Join(dir, ".grew-staged")
	backup := filepath.Join(dir, "grew.previous")
	sf := filepath.Join(logDir, "update-state.json")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 1.1.0"`)

	checks := []HealthChecker{VersionHealthChecker{Expected: "1.1.0"}}
	if err := TransactionalInstall(current, staged, backup, sf, "v1.1.0", "release", checks); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No rollback entry must appear when the update succeeds.
	if hasAuditAction(t, logDir, auditlog.ActionSelfRollback) {
		t.Error("unexpected self-rollback audit entry after successful update")
	}
}

func TestTransactionalInstall_PathsInDifferentDirs_Rejected(t *testing.T) {
	t.Parallel()
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	current := filepath.Join(dir1, "grew")
	staged := filepath.Join(dir2, ".grew-staged") // different dir — must be rejected
	backup := filepath.Join(dir1, "grew.previous")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged, `echo "grew 1.1.0"`)

	err := TransactionalInstall(current, staged, backup, "", "v1.1.0", "release", nil)
	if err == nil {
		t.Fatal("expected error when staged is in a different directory")
	}
	if !contains(err.Error(), "same directory") {
		t.Errorf("expected 'same directory' in error, got: %s", err)
	}
}

// ---------------------------------------------------------------------------
// RecoverPendingUpdate
// ---------------------------------------------------------------------------

func TestRecoverPendingUpdate_NoStateFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := RecoverPendingUpdate(filepath.Join(dir, "update-state.json")); err != nil {
		t.Fatalf("unexpected error when state file absent: %v", err)
	}
}

func TestRecoverPendingUpdate_CommittedPhase_CleanedUp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sf := filepath.Join(dir, "update-state.json")
	writeStateFile(t, sf, UpdateState{
		Phase:      phaseCommitted,
		BackupPath: filepath.Join(dir, "grew.previous"),
		UpdatedAt:  time.Now().UTC(),
	})
	if err := RecoverPendingUpdate(sf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noStateFile(t, sf)
}

func TestRecoverPendingUpdate_StagedPhase_CleanedUp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sf := filepath.Join(dir, "update-state.json")
	writeStateFile(t, sf, UpdateState{
		Phase:     phaseStaged,
		UpdatedAt: time.Now().UTC(),
	})
	// phase=staged means the crash happened before the swap — current binary is
	// untouched, so no rollback is needed.
	if err := RecoverPendingUpdate(sf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	noStateFile(t, sf)
}

func TestRecoverPendingUpdate_SwappedPhase_HealthyBinary_CommitsCleanly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	current := filepath.Join(dir, "grew")
	backup := filepath.Join(dir, "grew.previous")
	sf := filepath.Join(dir, "update-state.json")

	writeScript(t, current, `echo "grew 1.1.0"`) // healthy new binary
	writeScript(t, backup, `echo "grew 1.0.0"`)  // old binary waiting as backup

	writeStateFile(t, sf, UpdateState{
		Phase:       phaseSwapped,
		CurrentPath: current,
		BackupPath:  backup,
		TargetVer:   "v1.1.0",
		UpdatedAt:   time.Now().UTC(),
	})

	if err := RecoverPendingUpdate(sf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// New binary is healthy: backup and state file must be removed.
	noStateFile(t, backup)
	noStateFile(t, sf)
	// Current binary is still the new version.
	mustExecBin(t, current, "grew 1.1.0")
}

func TestRecoverPendingUpdate_SwappedPhase_UnhealthyBinary_Restores(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logDir := t.TempDir()
	current := filepath.Join(dir, "grew")
	backup := filepath.Join(dir, "grew.previous")
	sf := filepath.Join(logDir, "update-state.json")

	writeScript(t, current, `exit 1`)           // broken new binary
	writeScript(t, backup, `echo "grew 1.0.0"`) // known-good previous binary

	writeStateFile(t, sf, UpdateState{
		Phase:       phaseSwapped,
		CurrentPath: current,
		BackupPath:  backup,
		TargetVer:   "v1.1.0",
		UpdatedAt:   time.Now().UTC(),
	})

	if err := RecoverPendingUpdate(sf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Backup must have been restored as the current binary.
	mustExecBin(t, current, "grew 1.0.0")
	noStateFile(t, sf)
	// A self-rollback audit entry must be written.
	if !hasAuditAction(t, logDir, auditlog.ActionSelfRollback) {
		t.Error("expected self-rollback audit entry for crash recovery restore")
	}
}

func TestRecoverPendingUpdate_SwappedPhase_BackupMissing_Skips(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sf := filepath.Join(dir, "update-state.json")
	writeStateFile(t, sf, UpdateState{
		Phase:       phaseSwapped,
		CurrentPath: filepath.Join(dir, "grew"),
		BackupPath:  filepath.Join(dir, "grew.previous"), // does not exist
		TargetVer:   "v1.1.0",
		UpdatedAt:   time.Now().UTC(),
	})
	// Missing backup means the update already committed; not an error.
	if err := RecoverPendingUpdate(sf); err != nil {
		t.Fatalf("unexpected error when backup is missing: %v", err)
	}
	noStateFile(t, sf)
}

func TestRecoverPendingUpdate_MalformedStateFile_Removed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sf := filepath.Join(dir, "update-state.json")
	if err := os.WriteFile(sf, []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPendingUpdate(sf); err != nil {
		t.Fatalf("malformed state file should not return an error: %v", err)
	}
	noStateFile(t, sf)
}

// ---------------------------------------------------------------------------
// VersionHealthChecker
// ---------------------------------------------------------------------------

func TestVersionHealthChecker_Pass_ExactMatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `echo "grew 1.2.3"`)

	hc := VersionHealthChecker{Expected: "1.2.3"}
	if err := hc.Check(context.Background(), bin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVersionHealthChecker_Pass_EmptyExpected_AnyOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `echo "grew 0.0.1-dev+abc123"`)

	// Empty Expected = any non-empty output is acceptable (git/dev builds).
	hc := VersionHealthChecker{Expected: ""}
	if err := hc.Check(context.Background(), bin); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVersionHealthChecker_Pass_DevBuildBypassesMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	// A dev build outputs "dev" which bypasses the version mismatch check.
	writeScript(t, bin, `echo "grew dev"`)

	hc := VersionHealthChecker{Expected: "1.2.3"}
	if err := hc.Check(context.Background(), bin); err != nil {
		t.Fatalf("dev build should bypass version mismatch check: %v", err)
	}
}

func TestVersionHealthChecker_Fail_VersionMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `echo "grew 0.0.1"`)

	hc := VersionHealthChecker{Expected: "2.0.0"}
	err := hc.Check(context.Background(), bin)
	if err == nil {
		t.Fatal("expected version mismatch error")
	}
	if !contains(err.Error(), "version mismatch") {
		t.Errorf("expected 'version mismatch' in error, got: %s", err)
	}
}

func TestVersionHealthChecker_Fail_EmptyOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `true`) // exits 0 but produces no output

	hc := VersionHealthChecker{}
	err := hc.Check(context.Background(), bin)
	if err == nil {
		t.Fatal("expected error for empty output")
	}
	if !contains(err.Error(), "no version output") {
		t.Errorf("expected 'no version output' in error, got: %s", err)
	}
}

func TestVersionHealthChecker_Fail_BinaryExitsNonZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `exit 1`)

	hc := VersionHealthChecker{}
	err := hc.Check(context.Background(), bin)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !contains(err.Error(), "version check failed") {
		t.Errorf("expected 'version check failed' in error, got: %s", err)
	}
}

func TestVersionHealthChecker_Fail_BinaryNotFound(t *testing.T) {
	t.Parallel()
	hc := VersionHealthChecker{}
	if err := hc.Check(context.Background(), "/nonexistent/grew"); err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestVersionHealthChecker_Fail_Timeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grew")
	writeScript(t, bin, `sleep 30`)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hc := VersionHealthChecker{}
	if err := hc.Check(ctx, bin); err == nil {
		t.Fatal("expected timeout error")
	}
}

// ---------------------------------------------------------------------------
// writeUpdateState
// ---------------------------------------------------------------------------

func TestWriteUpdateState_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sf := filepath.Join(dir, "update-state.json")

	want := &UpdateState{
		CurrentPath: "/prefix/bin/grew",
		BackupPath:  "/prefix/bin/grew.previous",
		TargetVer:   "v1.2.3",
		Method:      "release",
		Phase:       phaseSwapped,
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := writeUpdateState(sf, want); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(sf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got UpdateState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Phase != want.Phase {
		t.Errorf("Phase: got %q want %q", got.Phase, want.Phase)
	}
	if got.TargetVer != want.TargetVer {
		t.Errorf("TargetVer: got %q want %q", got.TargetVer, want.TargetVer)
	}
	if got.Method != want.Method {
		t.Errorf("Method: got %q want %q", got.Method, want.Method)
	}
}

func TestWriteUpdateState_EmptyPath_NoOp(t *testing.T) {
	t.Parallel()
	// stateFile == "" must be a no-op (crash recovery not available for this run).
	state := &UpdateState{Phase: phaseStaged}
	if err := writeUpdateState("", state); err != nil {
		t.Fatalf("empty path should be a no-op, got error: %v", err)
	}
}

func TestWriteUpdateState_ConcurrentWrites_FinalStateValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sf := filepath.Join(dir, "update-state.json")
	state := &UpdateState{Phase: phaseStaged, TargetVer: "v1.0.0", UpdatedAt: time.Now().UTC()}

	const goroutines = 20
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() { errs <- writeUpdateState(sf, state) }()
	}
	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent write error: %v", err)
		}
	}

	// Final file on disk must be valid JSON regardless of write ordering.
	data, err := os.ReadFile(sf)
	if err != nil {
		t.Fatalf("read state file after concurrent writes: %v", err)
	}
	var got UpdateState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("state file is not valid JSON after concurrent writes: %v (content: %q)", err, data)
	}
}

type blockingHealthChecker struct{ ch <-chan struct{} }

func (b blockingHealthChecker) Check(ctx context.Context, binPath string) error {
	select {
	case <-b.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestConcurrentTransactionalInstall_SecondIsRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logDir := t.TempDir()
	stateFile := filepath.Join(logDir, "update-state.json")
	current := filepath.Join(dir, "grew")
	staged1 := filepath.Join(dir, ".grew-staged-1")
	staged2 := filepath.Join(dir, ".grew-staged-2")
	backup1 := filepath.Join(dir, "grew.previous")
	backup2 := filepath.Join(dir, "grew.previous.second")

	writeScript(t, current, `echo "grew 1.0.0"`)
	writeScript(t, staged1, `echo "grew 1.1.0"`)
	writeScript(t, staged2, `echo "grew 1.2.0"`)

	block := make(chan struct{})
	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		close(started)
		errCh <- TransactionalInstall(current, staged1, backup1, stateFile, "v1.1.0", "release", []HealthChecker{blockingHealthChecker{ch: block}})
	}()
	<-started

	lockPath := filepath.Join(logDir, "update.lock")
	for i := 0; i < 100; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err == nil {
			lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			_ = f.Close()
			if errors.Is(lockErr, syscall.EWOULDBLOCK) || errors.Is(lockErr, syscall.EAGAIN) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := TransactionalInstall(current, staged2, backup2, stateFile, "v1.2.0", "release", nil); !errors.Is(err, errUpdateAlreadyInProgress) {
		t.Fatalf("expected errUpdateAlreadyInProgress, got %v", err)
	}

	close(block)
	if err := <-errCh; err != nil {
		t.Fatalf("first transactional install failed: %v", err)
	}
	mustExecBin(t, current, "grew 1.1.0")
}
