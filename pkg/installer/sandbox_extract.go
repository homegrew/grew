package installer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/pkg/downloader"
	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/sandbox"
)

// sandboxedExtract runs archive extraction inside a sandboxed subprocess.
// The grew binary re-execs itself with the hidden "_extract" command,
// wrapped in a platform-specific sandbox that restricts writes to stageDir
// and denies network access. If the sandbox is unavailable, falls back to
// direct in-process extraction.

type ExtractArgs struct {
	ArchivePath string              `json:"archive_path"`
	DestDir     string              `json:"dest_dir"`
	Spec        formula.InstallSpec `json:"spec"`
}

func SandboxedExtract(archivePath, stageDir string, spec formula.InstallSpec) error {
	exe, err := os.Executable()
	if err != nil {
		// Can't locate ourselves — fall back to direct extraction.
		slog.Debug(fmt.Sprintf("cannot locate executable for sandboxed extract, falling back: %v", err))
		return downloader.Extract(archivePath, stageDir, spec)
	}

	// Resolve the stage directory to its canonical path to ensure consistency
	// between the parent and sandboxed subprocess, particularly on macOS
	// where /var is a symlink to /private/var.
	if eval, err := filepath.EvalSymlinks(stageDir); err == nil {
		stageDir = eval
	}
	stageDir = filepath.Clean(stageDir)

	cfg := sandbox.ExtractConfig{
		ArchiveFile: archivePath,
		StageDir:    stageDir,
	}

	args := ExtractArgs{
		ArchivePath: archivePath,
		DestDir:     stageDir,
		Spec:        spec,
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal extract args: %w", err)
	}

	// Ensure the stage directory exists so the subprocess can use it as cwd.
	// The extract.go validation checks that dest_dir is within cwd.
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return fmt.Errorf("create stage dir: %w", err)
	}

	cmd := sandbox.ExtractCommand(cfg, exe, "_extract")
	cmd.Dir = stageDir
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	slog.Debug(fmt.Sprintf("sandboxed extract: %s _extract (sandbox: %s)", exe, stageDir))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sandboxed extraction failed: %w", err)
	}
	return nil
}
