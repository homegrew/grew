package cmd

import (
	"flag"
	"fmt"
	"log/slog"

	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/linkage"
)

func runLinkage(args []string) error {
	fs := flag.NewFlagSet("linkage", flag.ContinueOnError)
	flags.Register(fs)
	test := fs.Bool("test", false, "Only report broken dependencies (exit 1 if any)")
	strict := fs.Bool("strict", false, "Also check for undeclared and unused dependencies")
	reverse := fs.Bool("reverse", false, "Show formulas that link against this formula's libraries")
	cached := fs.Bool("cached", false, "Use cached linkage results if available")
	quiet := fs.Bool("quiet", false, "Only output broken dependencies")
	fs.BoolVar(quiet, "q", false, "Only output broken dependencies")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	if *reverse && *test {
		return fmt.Errorf("--reverse and --test are mutually exclusive")
	}
	if *reverse && *strict {
		return fmt.Errorf("--reverse and --strict are mutually exclusive")
	}

	remaining := fs.Args()
	if len(remaining) != 1 {
		return fmt.Errorf("usage: grew linkage [--test] [--strict] [--reverse] [--cached] [-q] <formula>")
	}

	name := remaining[0]

	ctx, err := newReadContext()
	if err != nil {
		return err
	}

	if !ctx.Cellar.IsInstalled(name) {
		return fmt.Errorf("%s is not installed", name)
	}

	version, err := ctx.Cellar.InstalledVersion(name)
	if err != nil {
		return err
	}

	kegPath, err := ctx.Cellar.KegPath(name, version)
	if err != nil {
		return err
	}

	if *reverse {
		result, err := linkage.Reverse(name, version, kegPath, ctx.Paths.Cellar)
		if err != nil {
			return fmt.Errorf("reverse linkage: %w", err)
		}
		fmt.Print(linkage.FormatReverseResult(result, *quiet))
		return nil
	}

	var result *linkage.Result

	if *cached {
		r, loadErr := linkage.LoadCache(kegPath)
		if loadErr != nil {
			return fmt.Errorf("load linkage cache: %w", loadErr)
		}
		if r != nil {
			slog.Info("using cached linkage")
			result = r
		}
	}

	if result == nil {
		r, checkErr := linkage.Check(name, version, kegPath, ctx.Paths.Cellar)
		if checkErr != nil {
			return fmt.Errorf("linkage check: %w", checkErr)
		}
		result = r

		if *cached {
			if saveErr := linkage.SaveCache(result); saveErr != nil {
				return fmt.Errorf("save linkage cache: %w", saveErr)
			}
		}
	}

	fmtOpts := linkage.FormatOpts{Test: *test, Quiet: *quiet}

	if *strict {
		f, err := ctx.Loader.LoadByName(name)
		if err != nil {
			return fmt.Errorf("load formula %s: %w", name, err)
		}
		sr := result.Strict(f.Dependencies)
		fmtOpts.Strict = &sr
	}

	fmt.Print(linkage.FormatResult(result, fmtOpts))

	if *test {
		if len(result.Broken()) > 0 {
			return fmt.Errorf("broken linkage found")
		}
		if *strict && fmtOpts.Strict != nil {
			if len(fmtOpts.Strict.Undeclared) > 0 || len(fmtOpts.Strict.Unused) > 0 {
				return fmt.Errorf("strict linkage check failed")
			}
		}
	}
	return nil
}
