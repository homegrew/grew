package link

import (
	"github.com/homegrew/grew/pkg/context"
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/linker"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

var (
	linkOverwrite bool
	linkDryRun    bool
	linkForce     bool
)

var Command = &cobra.Command{
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
	RunE: func(c *cobra.Command, args []string) error {
		return runLink(args)
	},
}

func init() {
	Command.Flags().BoolVar(&linkOverwrite, "overwrite", false, "Overwrite existing files or symlinks from other formulas")
	Command.Flags().BoolVarP(&linkDryRun, "dry-run", "n", false, "Show what would be linked without making changes")
	Command.Flags().BoolVar(&linkForce, "force", false, "Link a keg-only formula into bin/, lib/, include/")
}

func runLink(args []string) error {
	slog.Debug("starting link command execution")

	if len(args) == 0 {
		return fmt.Errorf("usage: grew link [--overwrite] [--dry-run] [--force] <formula>...")
	}

	ctx, err := context.New()
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
			fmt.Fprintf(os.Stdout, "%s %s is keg-only. Use --force to link anyway.\n", ui.ArrowWarning(os.Stdout), name)
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
			ui.FprintArrow(os.Stderr, "%s %s linked", name, ver)
		}
	}
	return nil
}
