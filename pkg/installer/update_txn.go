package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/homegrew/grew/pkg/auditlog"
	"github.com/homegrew/grew/pkg/config"
	"github.com/homegrew/grew/pkg/safepath"
)

// updatePhase tracks where in the transaction lifecycle the process is.
// Written atomically to disk so a crash between swap and commit can be
// detected and recovered on the next startup via RecoverPendingUpdate.
type updatePhase string

const (
	phaseStaged    updatePhase = "staged"
	phaseSwapped   updatePhase = "swapped"
	phaseCommitted updatePhase = "committed"
)

// UpdateState is persisted atomically to <prefix>/var/log/update-state.json.
// If the process crashes after phaseSwapped but before phaseCommitted,
// RecoverPendingUpdate restores the backup binary automatically.
type UpdateState struct {
	CurrentPath string      `json:"current_path"`
	BackupPath  string      `json:"backup_path"`
	TargetVer   string      `json:"target_version"`
	Method      string      `json:"method"` // "patch", "release", "source"
	Phase       updatePhase `json:"phase"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// HealthChecker is implemented by every post-swap check.
// binPath is always the live installed path (CurrentPath), never the staged one.
type HealthChecker interface {
	Check(ctx context.Context, binPath string) error
}

// VersionHealthChecker runs `<bin> version` and validates the output.
// It mirrors VerifyBinaryIntegrity but honours a context timeout and is
// composable with other HealthChecker implementations.
type VersionHealthChecker struct {
	// Expected version string (without "v" prefix).
	// Empty means any non-empty output is accepted (e.g. git/dev builds).
	Expected string
}

// Check implements HealthChecker.
func (v VersionHealthChecker) Check(ctx context.Context, binPath string) error {
	cmd := exec.CommandContext(ctx, binPath, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("version check failed: %w\noutput: %s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return fmt.Errorf("no version output")
	}
	if v.Expected != "" && !strings.Contains(s, v.Expected) && !strings.Contains(s, "dev") {
		return fmt.Errorf("version mismatch: got %q want %q", s, v.Expected)
	}
	slog.Debug("post-swap version check passed", "output", s)
	return nil
}

var errUpdateAlreadyInProgress = errors.New("update already in progress")

func acquireUpdateLock(logDir string) (*os.File, func(), error) {
	trustedBase := filepath.Clean(config.Default().Log)
	trustedBaseAbs, err := filepath.Abs(trustedBase)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve trusted log directory: %w", err)
	}

	logDirAbs, err := filepath.Abs(filepath.Clean(logDir))
	if err != nil {
		return nil, nil, fmt.Errorf("resolve lock directory: %w", err)
	}
	rel, err := filepath.Rel(trustedBaseAbs, logDirAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, nil, fmt.Errorf("invalid lock directory %q: must be within %q", logDir, trustedBaseAbs)
	}

	if err := os.MkdirAll(logDirAbs, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create lock directory: %w", err)
	}
	lockPath := filepath.Join(logDirAbs, "update.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open update lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close update lock file: %w", closeErr))
		}
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, nil, errUpdateAlreadyInProgress
		}
		return nil, nil, fmt.Errorf("lock update file: %w", err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		if err := f.Close(); err != nil {
			slog.Warn("close update lock file", "error", err)
		}
	}
	return f, release, nil
}

// UpdateStateFilePath returns the canonical path for the update state file.
// Using config.Default().Log keeps it inside the managed prefix,
// consistent with the audit log location.
// Returns empty string when the configured log directory is invalid/unsafe.
func UpdateStateFilePath() string {
	logDir := config.Default().Log
	if abs, err := filepath.Abs(logDir); err == nil {
		logDir = filepath.Clean(abs)
	} else {
		logDir = filepath.Clean(logDir)
	}
	if err := safepath.SafeAbsolutePath(logDir); err != nil {
		slog.Warn("invalid log directory for update state file", "logDir", logDir, "err", err)
		return ""
	}

	stateFile := filepath.Join(logDir, "update-state.json")
	if abs, err := filepath.Abs(stateFile); err == nil {
		stateFile = filepath.Clean(abs)
	} else {
		stateFile = filepath.Clean(stateFile)
	}
	if err := safepath.SafeAbsolutePath(stateFile); err != nil {
		slog.Warn("invalid update state file path", "stateFile", stateFile, "err", err)
		return ""
	}
	if rel, err := filepath.Rel(logDir, stateFile); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		slog.Warn("update state file escapes log directory", "logDir", logDir, "stateFile", stateFile)
		return ""
	}
	return stateFile
}

// TransactionalInstall performs a crash-safe, rollback-capable binary swap.
//
// Flow:
//
//  1. Validate all paths with safepath.
//  2. Write state file at phase=staged.
//  3. Rename currentPath → backupPath  (backup existing binary).
//  4. Rename stagedPath  → currentPath (activate new binary).
//  5. Write state file at phase=swapped.
//  6. Run each postCheck against currentPath with a 10-second timeout.
//     7a. All pass  → remove backup, remove state file (committed).
//     7b. Any fails → rename backupPath → currentPath (rollback), log to audit.
//
// If the process crashes between steps 4 and 7, RecoverPendingUpdate detects
// the orphaned state file on the next startup and restores the backup.
//
// currentPath, stagedPath, and backupPath must all reside in the same
// directory so that the rename operations are atomic (same filesystem).
//
// stateFile may be empty to disable crash-recovery persistence (e.g. tests).
func TransactionalInstall(
	currentPath, stagedPath, backupPath, stateFile string,
	targetVer, method string,
	postChecks []HealthChecker,
) (retErr error) {
	// --- path validation -------------------------------------------------------
	for label, p := range map[string]string{
		"current": currentPath,
		"staged":  stagedPath,
		"backup":  backupPath,
		"state":   stateFile,
	} {
		// stateFile may be empty to disable persistence in tests/callers.
		if label == "state" && p == "" {
			continue
		}
		if err := safepath.SafeAbsolutePath(p); err != nil {
			return fmt.Errorf("invalid %s path %q: %w", label, p, err)
		}
	}
	if stateFile != "" {
		if err := safepath.SafeAbsolutePath(stateFile); err != nil {
			return fmt.Errorf("invalid state file path %q: %w", stateFile, err)
		}
		if filepath.Base(stateFile) != "update-state.json" {
			return fmt.Errorf("state file %q must use canonical name update-state.json", stateFile)
		}

		canonicalStateFile := filepath.Clean(UpdateStateFilePath())
		resolvedStateFile := filepath.Clean(stateFile)
		if canonicalStateFile != resolvedStateFile {
			return fmt.Errorf("state file %q must match canonical path %q", stateFile, canonicalStateFile)
		}
	}

	// All three paths must be in the same directory for atomic rename.
	dir := filepath.Dir(currentPath)
	for label, p := range map[string]string{
		"staged": stagedPath,
		"backup": backupPath,
	} {
		if filepath.Dir(p) != dir {
			return fmt.Errorf("%s path %q must be in the same directory as current (%s)", label, p, dir)
		}
	}

	if stateFile != "" {
		lockFile, releaseLock, err := acquireUpdateLock(logDirFor(stateFile))
		if err != nil {
			return fmt.Errorf("acquire update lock: %w", err)
		}
		defer releaseLock()
		_ = lockFile
	}

	// --- persist initial state ------------------------------------------------
	state := &UpdateState{
		CurrentPath: currentPath,
		BackupPath:  backupPath,
		TargetVer:   targetVer,
		Method:      method,
		Phase:       phaseStaged,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := writeUpdateState(stateFile, state); err != nil {
		// Non-fatal: crash recovery won't be available for this run.
		slog.Warn("could not write update state file", "err", err)
	}

	// --- step 1: backup current binary ----------------------------------------
	if err := os.Rename(currentPath, backupPath); err != nil {
		_ = os.Remove(stateFile)
		return fmt.Errorf("backup current binary: %w", err)
	}

	// --- step 2: activate staged binary ---------------------------------------
	if err := os.Rename(stagedPath, currentPath); err != nil {
		// Staged rename failed — restore backup before returning.
		if rerr := os.Rename(backupPath, currentPath); rerr != nil {
			_ = os.Remove(stateFile)
			return fmt.Errorf("activate staged binary: %w; backup restore also failed: %v", err, rerr)
		}
		_ = os.Remove(stateFile)
		return fmt.Errorf("activate staged binary: %w", err)
	}

	state.Phase = phaseSwapped
	_ = writeUpdateState(stateFile, state)

	// --- deferred rollback: triggered if any post-check or commit returns error
	defer func() {
		if retErr != nil {
			slog.Warn("post-swap check failed — rolling back", "err", retErr)
			if rerr := restoreBackup(currentPath, backupPath); rerr != nil {
				retErr = fmt.Errorf("%w; rollback also failed: %v", retErr, rerr)
				auditlog.New(logDirFor(stateFile)).Log(
					auditlog.ActionSelfRollback, "grew", targetVer, "",
					fmt.Sprintf("rollback failed: %v", rerr))
			} else {
				auditlog.New(logDirFor(stateFile)).Log(
					auditlog.ActionSelfRollback, "grew", targetVer, "",
					"rolled back after post-swap check failure")
			}
			_ = os.Remove(stateFile)
		}
	}()

	// --- step 3: post-swap health checks --------------------------------------
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, hc := range postChecks {
		if err := hc.Check(ctx, currentPath); err != nil {
			return fmt.Errorf("post-swap health check: %w", err)
		}
	}

	// --- step 4: commit -------------------------------------------------------
	_ = os.Remove(backupPath)
	_ = os.Remove(stateFile)
	return nil
}

// RecoverPendingUpdate checks for an orphaned update-state.json file, which
// indicates a process crash between swap and commit. If found and the installed
// binary is unhealthy, the backup is restored automatically.
//
// Call this early in main startup — before any update command runs — so that
// the binary is always in a known-good state when the user's command executes.
func RecoverPendingUpdate(stateFile string) error {
	lockFile, releaseLock, err := acquireUpdateLock(logDirFor(stateFile))
	if err != nil {
		if errors.Is(err, errUpdateAlreadyInProgress) {
			slog.Debug("update lock held — skipping crash recovery")
			return nil
		}
		return fmt.Errorf("acquire update lock: %w", err)
	}
	defer releaseLock()
	_ = lockFile

	data, err := os.ReadFile(stateFile)
	if os.IsNotExist(err) {
		return nil // nothing to recover
	}
	if err != nil {
		return fmt.Errorf("read update state: %w", err)
	}

	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		slog.Warn("malformed update-state.json — removing", "err", err)
		_ = os.Remove(stateFile)
		return nil
	}

	// Only phaseSwapped indicates a potential crash between swap and commit.
	if state.Phase != phaseSwapped {
		_ = os.Remove(stateFile)
		return nil
	}

	if err := safepath.SafeAbsolutePath(state.CurrentPath); err != nil {
		slog.Warn("invalid current path in update state — removing", "path", state.CurrentPath, "err", err)
		_ = os.Remove(stateFile)
		return nil
	}
	if err := safepath.SafeAbsolutePath(state.BackupPath); err != nil {
		slog.Warn("invalid backup path in update state — removing", "path", state.BackupPath, "err", err)
		_ = os.Remove(stateFile)
		return nil
	}

	currentPath := state.CurrentPath
	backupPath := state.BackupPath
	expectedBackupPath := filepath.Join(filepath.Dir(currentPath), filepath.Base(currentPath)+".previous")
	if backupPath != expectedBackupPath {
		slog.Warn("invalid backup path in update state — removing",
			"backup", backupPath, "expected", expectedBackupPath)
		_ = os.Remove(stateFile)
		return nil
	}

	// If backup no longer exists, the commit already happened elsewhere.
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		_ = os.Remove(stateFile)
		return nil
	}

	// Run a quick health check on the currently installed binary.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hc := VersionHealthChecker{Expected: strings.TrimPrefix(state.TargetVer, "v")}
	if err := hc.Check(ctx, currentPath); err != nil {
		slog.Warn("crashed update left unhealthy binary — restoring backup",
			"current", currentPath, "backup", backupPath, "err", err)
		if rerr := restoreBackup(currentPath, backupPath); rerr != nil {
			_ = os.Remove(stateFile)
			return fmt.Errorf("crash recovery restore failed: %w", rerr)
		}
		auditlog.New(logDirFor(stateFile)).Log(
			auditlog.ActionSelfRollback, "grew", state.TargetVer, "",
			"crash recovery: restored previous binary")
	} else {
		// New binary is healthy — the update succeeded; just clean up.
		_ = os.Remove(backupPath)
	}

	_ = os.Remove(stateFile)
	return nil
}

// logDirFor returns the directory to use for audit logging.
// When stateFile is non-empty the audit log is written to the same directory
// (state file and audit log are co-located under <prefix>/var/log).
// Falls back to config.Default().Log when stateFile is empty.
func logDirFor(stateFile string) string {
	if stateFile != "" {
		return filepath.Dir(stateFile)
	}
	return config.Default().Log
}

// restoreBackup moves backupPath back to currentPath.
// It removes currentPath first to handle the case where a partial binary
// was left in place after a failed write.
func restoreBackup(currentPath, backupPath string) error {
	currentPath = filepath.Clean(currentPath)
	backupPath = filepath.Clean(backupPath)

	if currentPath == "." || currentPath == "" || backupPath == "." || backupPath == "" {
		return fmt.Errorf("invalid rollback paths: current=%q backup=%q", currentPath, backupPath)
	}
	if !filepath.IsAbs(currentPath) || !filepath.IsAbs(backupPath) {
		return fmt.Errorf("rollback paths must be absolute: current=%q backup=%q", currentPath, backupPath)
	}
	if filepath.Dir(currentPath) != filepath.Dir(backupPath) {
		return fmt.Errorf("rollback paths must share directory: current=%q backup=%q", currentPath, backupPath)
	}
	if backupPath != currentPath+".bak" {
		return fmt.Errorf("rollback backup path mismatch: current=%q backup=%q", currentPath, backupPath)
	}

	baseDir := filepath.Dir(currentPath)

	safeCurrent, err := safepath.ResolveWithin(baseDir, currentPath)
	if err != nil {
		return fmt.Errorf("validate current path %q within %q: %w", currentPath, baseDir, err)
	}
	safeBackup, err := safepath.ResolveWithin(baseDir, backupPath)
	if err != nil {
		return fmt.Errorf("validate backup path %q within %q: %w", backupPath, baseDir, err)
	}

	_ = os.Remove(safeCurrent)
	if err := os.Rename(safeBackup, safeCurrent); err != nil {
		return fmt.Errorf("rename backup %q to current %q: %w", safeBackup, safeCurrent, err)
	}
	return nil
}

// writeUpdateState atomically persists s to stateFile using the
// temp-file-plus-rename pattern used throughout the grew codebase.
// A no-op when stateFile is empty.
func writeUpdateState(stateFile string, s *UpdateState) error {
	if stateFile == "" {
		return nil
	}
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal update state: %w", err)
	}
	dir := filepath.Dir(stateFile)
	tmp, err := os.CreateTemp(dir, ".update-state-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, stateFile); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp state file: %w", err)
	}
	return nil
}
