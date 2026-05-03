package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/homegrew/grew/internal/linkage"
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

var (
	linkageTest    bool
	linkageStrict  bool
	linkageReverse bool
	linkageCached  bool
	linkageQuiet   bool
)

var LinkageCmd = &cobra.Command{
	Use:   "linkage [options] <formula ...>",
	Short: "Check dynamic library dependencies",
	Long: `Check dynamic library dependencies of installed formulas.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return RunLinkage(args)
	},
}

func init() {
	LinkageCmd.Flags().BoolVar(&linkageTest, "test", false, "Only report broken dependencies (exit 1 if any)")
	LinkageCmd.Flags().BoolVar(&linkageStrict, "strict", false, "Also check for undeclared and unused dependencies")
	LinkageCmd.Flags().BoolVar(&linkageReverse, "reverse", false, "Show formulas that link against this formula's libraries")
	LinkageCmd.Flags().BoolVar(&linkageCached, "cached", false, "Use cached linkage results if available")
	LinkageCmd.Flags().BoolVarP(&linkageQuiet, "quiet", "q", false, "Only output broken dependencies")
	rootCmd.AddCommand(LinkageCmd)
}

func RunLinkage(args []string) error {
	slog.Debug("starting linkage command execution")
	
	if linkageReverse && linkageTest {
		return fmt.Errorf("--reverse and --test are mutually exclusive")
	}
	if linkageReverse && linkageStrict {
		return fmt.Errorf("--reverse and --strict are mutually exclusive")
	}

	remaining := args
	if len(remaining) == 0 {
		return fmt.Errorf("usage: grew linkage [--test] [--strict] [--reverse] [--cached] [-q] <formula>...")
	}

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	var hasErrors bool

	for _, name := range remaining {
		if !ctx.Cellar.IsInstalled(name) {
			slog.Warn(fmt.Sprintf("%s is not installed", name))
			continue
		}

		version, err := ctx.Cellar.InstalledVersion(name)
		if err != nil {
			return err
		}

		kegPath, err := ctx.Cellar.KegPath(name, version)
		if err != nil {
			return err
		}

		if linkageReverse {
			result, err := linkage.Reverse(name, version, kegPath, ctx.Paths.Cellar)
			if err != nil {
				return fmt.Errorf("reverse linkage: %w", err)
			}
			if len(remaining) > 1 && !linkageQuiet {
				ui.FprintArrow(os.Stderr, "Reverse linkage for %s", name)
			}
			fmt.Print(linkage.FormatReverseResult(result, linkageQuiet))
			continue
		}

		var result *linkage.Result

		if linkageCached {
			r, loadErr := linkage.LoadCache(kegPath)
			if loadErr != nil {
				return fmt.Errorf("load linkage cache: %w", loadErr)
			}
			if r != nil {
				slog.Info("using cached linkage for " + name)
				result = r
			}
		}

		if result == nil {
			r, checkErr := linkage.Check(name, version, kegPath, ctx.Paths.Cellar)
			if checkErr != nil {
				return fmt.Errorf("linkage check: %w", checkErr)
			}
			result = r

			if linkageCached {
				if saveErr := linkage.SaveCache(result); saveErr != nil {
					return fmt.Errorf("save linkage cache: %w", saveErr)
				}
			}
		}

		fmtOpts := linkage.FormatOpts{Test: linkageTest, Quiet: linkageQuiet}

		if linkageStrict {
			f, err := ctx.Loader.LoadByName(name)
			if err != nil {
				return fmt.Errorf("load formula %s: %w", name, err)
			}
			sr := result.Strict(f.Dependencies)
			fmtOpts.Strict = &sr
		}

		if len(remaining) > 1 && !linkageQuiet {
			ui.FprintArrow(os.Stderr, "Linkage for %s", name)
		}
		fmt.Print(linkage.FormatResult(result, fmtOpts))

		if linkageTest {
			if len(result.Broken()) > 0 {
				hasErrors = true
			}
			if linkageStrict && fmtOpts.Strict != nil {
				if len(fmtOpts.Strict.Undeclared) > 0 || len(fmtOpts.Strict.Unused) > 0 {
					hasErrors = true
				}
			}
		}
	}

	if linkageTest && hasErrors {
		return fmt.Errorf("linkage check failed")
	}
	return nil
}
