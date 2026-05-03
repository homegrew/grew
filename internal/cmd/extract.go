package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/spf13/cobra"
)

// extractArgs is the JSON payload passed via stdin to the sandboxed extraction subprocess.
type extractArgs struct {
	ArchivePath string              `json:"archive_path"`
	DestDir     string              `json:"dest_dir"`
	Spec        formula.InstallSpec `json:"spec"`
}

var ExtractCmd = &cobra.Command{
	Use:    "_extract",
	Short:  "Internal hidden extraction command",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Debug("starting extract command execution")
		var extArgs extractArgs
		if err := json.NewDecoder(os.Stdin).Decode(&extArgs); err != nil {
			return fmt.Errorf("decode extract args: %w", err)
		}

		if extArgs.ArchivePath == "" || extArgs.DestDir == "" {
			return fmt.Errorf("archive_path and dest_dir are required")
		}

		// Constrain dest_dir to the current working directory to avoid writing
		// outside the sandboxed extraction tree.
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determine working directory: %w", err)
		}
		cwdAbs, err := filepath.Abs(cwd)
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
		if eval, err := filepath.EvalSymlinks(cwdAbs); err == nil {
			cwdAbs = eval
		}
		cwdAbs = filepath.Clean(cwdAbs)

		destAbs, err := filepath.Abs(extArgs.DestDir)
		if err != nil {
			return fmt.Errorf("resolve dest_dir: %w", err)
		}
		if eval, err := filepath.EvalSymlinks(destAbs); err == nil {
			destAbs = eval
		}
		destAbs = filepath.Clean(destAbs)

		if err := safepath.CheckSubpath(cwdAbs, destAbs); err != nil {
			return fmt.Errorf("dest_dir %q escapes working directory %q: %w", destAbs, cwdAbs, err)
		}
		return downloader.Extract(extArgs.ArchivePath, destAbs, extArgs.Spec)
	},
}

func init() {
	rootCmd.AddCommand(ExtractCmd)
}
