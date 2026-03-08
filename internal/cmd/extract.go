package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
)

// extractArgs is the JSON payload passed via stdin to the sandboxed extraction subprocess.
type extractArgs struct {
	ArchivePath string             `json:"archive_path"`
	DestDir     string             `json:"dest_dir"`
	Spec        formula.InstallSpec `json:"spec"`
}

// runExtract is the hidden "_extract" command used internally by sandboxed extraction.
// It reads a JSON payload from stdin and extracts the archive. This function is
// invoked by grew re-execing itself inside a sandbox.
func runExtract(_ []string) error {
	var args extractArgs
	if err := json.NewDecoder(os.Stdin).Decode(&args); err != nil {
		return fmt.Errorf("decode extract args: %w", err)
	}

	if args.ArchivePath == "" || args.DestDir == "" {
		return fmt.Errorf("archive_path and dest_dir are required")
	}

	return downloader.Extract(args.ArchivePath, args.DestDir, args.Spec)
}
