package homepage

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"

	"github.com/homegrew/grew/internal/context"
	"github.com/spf13/cobra"
)

var homepageCask bool

var Command = &cobra.Command{
	Use:   "homepage [formula ...]",
	Short: "Open a formula's homepage in a browser",
	Long:  `Open the homepage for one or more formulas in your default web browser.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHomepage(args)
	},
}

func init() {
	Command.Flags().BoolVar(&homepageCask, "cask", false, "Open a cask's homepage")
}

func runHomepage(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: grew homepage [--cask] <formula|cask>...")
	}

	ctx, err := context.New()
	if err != nil {
		return err
	}

	for _, name := range args {
		var url string
		if homepageCask {
			c, err := ctx.LoadCask(name)
			if err != nil {
				return err
			}
			url = c.Homepage
		} else {
			f, err := ctx.LoadFormula(name)
			if err != nil {
				return err
			}
			url = f.Homepage
		}

		if url == "" {
			slog.Warn("No homepage defined", "name", name)
			continue
		}

		slog.Info("Opening homepage", "name", name, "url", url)
		if err := openURL(url); err != nil {
			return fmt.Errorf("open %q: %w", url, err)
		}
	}

	return nil
}

func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	default: // linux, freebsd, etc.
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Run()
}
