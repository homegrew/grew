package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"net/http"

	"github.com/homegrew/grew/cmd/alias"
	"github.com/homegrew/grew/cmd/audit"
	"github.com/homegrew/grew/cmd/autoremove"
	cmdcache "github.com/homegrew/grew/cmd/cache"
	"github.com/homegrew/grew/cmd/cleanup"
	"github.com/homegrew/grew/cmd/completion"
	"github.com/homegrew/grew/cmd/config"
	"github.com/homegrew/grew/cmd/deps"
	"github.com/homegrew/grew/cmd/doctor"
	"github.com/homegrew/grew/cmd/extract"
	"github.com/homegrew/grew/cmd/info"
	"github.com/homegrew/grew/cmd/install"
	"github.com/homegrew/grew/cmd/leaves"
	"github.com/homegrew/grew/cmd/link"
	"github.com/homegrew/grew/cmd/linkage"
	"github.com/homegrew/grew/cmd/list"
	"github.com/homegrew/grew/cmd/lock"
	"github.com/homegrew/grew/cmd/pin"
	"github.com/homegrew/grew/cmd/reinstall"
	"github.com/homegrew/grew/cmd/resetupdate"
	"github.com/homegrew/grew/cmd/search"
	"github.com/homegrew/grew/cmd/services"
	"github.com/homegrew/grew/cmd/setup"
	"github.com/homegrew/grew/cmd/shellenv"
	"github.com/homegrew/grew/cmd/sign"
	"github.com/homegrew/grew/cmd/tap"
	"github.com/homegrew/grew/cmd/uninstall"
	"github.com/homegrew/grew/cmd/unlink"
	"github.com/homegrew/grew/cmd/unpin"
	"github.com/homegrew/grew/cmd/untap"
	"github.com/homegrew/grew/cmd/update"
	"github.com/homegrew/grew/cmd/upgrade"
	"github.com/homegrew/grew/cmd/verify"
	"github.com/homegrew/grew/cmd/version"
	"github.com/homegrew/grew/cmd/vulnscan"
	intcmd "github.com/homegrew/grew/internal/cmd"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/osvdev"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/runtime"
	intver "github.com/homegrew/grew/internal/version"
	"github.com/spf13/cobra"
)

var Grew = &cobra.Command{
	Use:     "grew",
	Short:   "A package manager written in Go",
	Long:    `grew is a high-performance, Go-based command-line package manager inspired by Homebrew.`,
	Version: intver.Version(),
	PersistentPreRun: func(c *cobra.Command, args []string) {
		// Apply flag implications and configure the logger.
		flags.Resolve()
	},
}

func init() {
	if apiBase := os.Getenv("HOMEGREW_GITHUB_API_BASE"); apiBase != "" {
		if !runtime.DevMode {
			fmt.Fprintf(os.Stderr, "grew: HOMEGREW_GITHUB_API_BASE requires devmode build\n")
			os.Exit(1)
		}
		if err := release.SetAPIBase(apiBase); err != nil {
			fmt.Fprintf(os.Stderr, "grew: %v\n", err)
			os.Exit(1)
		}
	}

	if apiBase := os.Getenv("HOMEGREW_OSV_API_BASE"); apiBase != "" {
		if !runtime.DevMode {
			fmt.Fprintf(os.Stderr, "grew: HOMEGREW_OSV_API_BASE requires devmode build\n")
			os.Exit(1)
		}
		if err := osvdev.SetAPIBase(apiBase); err != nil {
			fmt.Fprintf(os.Stderr, "grew: %v\n", err)
			os.Exit(1)
		}
	}

	if certFile := os.Getenv("HOMEGREW_TEST_CERT_FILE"); certFile != "" {
		if !runtime.DevMode {
			fmt.Fprintf(os.Stderr, "grew: HOMEGREW_TEST_CERT_FILE requires devmode build\n")
			os.Exit(1)
		}
		certPEM, err := os.ReadFile(certFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read test cert: %v\n", err)
			os.Exit(1)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pool.AppendCertsFromPEM(certPEM)

		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
		http.DefaultTransport = transport
	}

	Grew.PersistentFlags().BoolVarP(&flags.Verbose, "verbose", "v", false, "Show detailed output")
	Grew.PersistentFlags().BoolVarP(&flags.Debug, "debug", "d", false, "Show debug diagnostics (implies --verbose)")
	Grew.PersistentFlags().BoolVarP(&flags.Quiet, "quiet", "q", false, "Only print errors")

	// Add refactored commands
	Grew.AddCommand(alias.Command)
	Grew.AddCommand(audit.Command)
	Grew.AddCommand(autoremove.Command)
	Grew.AddCommand(cmdcache.Command)
	Grew.AddCommand(cleanup.Command)
	Grew.AddCommand(completion.Command)
	Grew.AddCommand(config.Command)
	Grew.AddCommand(deps.Command)
	Grew.AddCommand(doctor.Command)
	Grew.AddCommand(extract.Command)
	Grew.AddCommand(info.Command)
	Grew.AddCommand(install.Command)
	Grew.AddCommand(leaves.Command)
	Grew.AddCommand(link.Command)
	Grew.AddCommand(linkage.Command)
	Grew.AddCommand(list.Command)
	Grew.AddCommand(lock.Command)
	Grew.AddCommand(pin.Command)
	Grew.AddCommand(reinstall.Command)
	Grew.AddCommand(resetupdate.Command)
	Grew.AddCommand(search.Command)
	Grew.AddCommand(services.Command)
	Grew.AddCommand(setup.Command)
	Grew.AddCommand(shellenv.Command)
	Grew.AddCommand(sign.Command)
	Grew.AddCommand(tap.Command)
	Grew.AddCommand(uninstall.Command)
	Grew.AddCommand(unlink.Command)
	Grew.AddCommand(unpin.Command)
	Grew.AddCommand(untap.Command)
	Grew.AddCommand(update.Command)
	Grew.AddCommand(upgrade.Command)
	Grew.AddCommand(upgrade.OutdatedCommand)
	Grew.AddCommand(verify.Command)
	Grew.AddCommand(version.Command)
	Grew.AddCommand(vulnscan.Command)

	// Add legacy commands from internal/cmd
	intcmd.AddLegacyCommands(Grew)
}

// Execute executes the root command.
func Execute(args []string) error {
	Grew.Version = intver.Version()
	Grew.SetArgs(args)
	return Grew.Execute()
}
