package cmd

import (
	"flag"
	"fmt"

	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/linkage"
)

func runLinkage(args []string) error {
	fs := flag.NewFlagSet("linkage", flag.ContinueOnError)
	flags.Register(fs)
	test := fs.Bool("test", false, "Only report broken dependencies (exit 1 if any)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	flags.Resolve()

	remaining := fs.Args()
	if len(remaining) != 1 {
		return fmt.Errorf("usage: grew linkage [--test] <formula>")
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

	result, err := linkage.Check(name, version, kegPath, ctx.Paths.Cellar)
	if err != nil {
		return fmt.Errorf("linkage check: %w", err)
	}

	fmt.Print(linkage.FormatResult(result, *test))

	if *test && len(result.Broken()) > 0 {
		return fmt.Errorf("broken linkage found")
	}
	return nil
}
