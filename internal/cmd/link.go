package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/linker"
	"github.com/spf13/cobra"
)

var (
	linkOverwrite bool
	linkDryRun    bool
	linkForce     bool
	unlinkDryRun  bool
)

var LinkCmd = &cobra.Command{
	Use:   "link [flags] <formula ...>",
	Short: "Create symlinks for formulas",
	Long: `Create symlinks for an installed formula. Symlinks binaries into bin/,
libraries into lib/, and headers into include/. Also creates an opt/
symlink pointing to the Cellar keg.

For keg-only formulas, only the opt/ symlink is created unless --force
is used.

Examples:
  grew link jq
  grew link --dry-run jq
  grew link --overwrite jq
  grew link --force openssl`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLink(args)
	},
}

var UnlinkCmd = &cobra.Command{
	Use:   "unlink [flags] <formula ...>",
	Short: "Remove symlinks for formulas",
	Long: `Remove symlinks for an installed formula without uninstalling it.

Examples:
  grew unlink jq`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUnlink(args)
	},
}

func init() {
	LinkCmd.Flags().BoolVar(&linkOverwrite, "overwrite", false, "Overwrite existing files or symlinks from other formulas")
	LinkCmd.Flags().BoolVarP(&linkDryRun, "dry-run", "n", false, "Show what would be linked without making changes")
	LinkCmd.Flags().BoolVar(&linkForce, "force", false, "Link a keg-only formula into bin/, lib/, include/")
	rootCmd.AddCommand(LinkCmd)

	UnlinkCmd.Flags().BoolVarP(&unlinkDryRun, "dry-run", "n", false, "Show what would be unlinked without making changes")
	rootCmd.AddCommand(UnlinkCmd)
}

func runLink(args []string) error {
	slog.Debug("starting link command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew link [--overwrite] [--dry-run] [--force] <formula>...")
	}

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	for _, name := range args {
		if !ctx.Cellar.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		ver, err := ctx.Cellar.InstalledVersion(name)
		if err != nil {
			return err
		}

		f, err := ctx.Loader.LoadByName(name)
		kegOnly := false
		if err == nil {
			kegOnly = f.KegOnly
		}

		if kegOnly && !linkForce {
			fmt.Printf("Warning: %s is keg-only. Use --force to link anyway.\n", name)
		}

		lnk := &linker.Linker{Paths: ctx.Paths}
		kegPath, _ := ctx.Cellar.KegPath(name, ver)
		slog.Info("keg: " + kegPath)
		opts := linker.LinkOpts{
			KegOnly:   kegOnly,
			Overwrite: linkOverwrite,
			DryRun:    linkDryRun,
			Force:     linkForce,
		}
		if err := lnk.LinkWithOpts(name, ver, opts); err != nil {
			return err
		}
		slog.Info(fmt.Sprintf("opt/%s -> %s", name, kegPath))
		if !kegOnly || linkForce {
			slog.Info("symlinked bin/, lib/, include/ contents")
		}

		if !linkDryRun {
			fmt.Fprintf(os.Stderr, "==> %s %s linked\n", name, ver)
		}
	}
	return nil
}

func runUnlink(args []string) error {
	slog.Debug("starting unlink command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew unlink [--dry-run] <formula>...")
	}

	paths := config.Default()
	cel := &cellar.Cellar{Path: paths.Cellar}
	lnk := &linker.Linker{Paths: paths}

	for _, name := range args {
		if !cel.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("formula %q is not installed", name))
			continue
		}

		if err := lnk.UnlinkWithOpts(name, linker.UnlinkOpts{DryRun: unlinkDryRun}); err != nil {
			return err
		}

		if unlinkDryRun {
			slog.Info("(dry run, no changes made)")
		} else {
			slog.Info("removed symlinks from bin/, lib/, include/, opt/")
			fmt.Fprintf(os.Stderr, "==> %s unlinked\n", name)
		}
	}
	return nil
}
