package cli

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/homegrew/grew/cmd/alias"
	"github.com/homegrew/grew/cmd/audit"
	"github.com/homegrew/grew/cmd/autoremove"
	cmdcache "github.com/homegrew/grew/cmd/cache"
	"github.com/homegrew/grew/cmd/cleanup"
	"github.com/homegrew/grew/cmd/config"
	"github.com/homegrew/grew/cmd/create"
	"github.com/homegrew/grew/cmd/deps"
	"github.com/homegrew/grew/cmd/desc"
	"github.com/homegrew/grew/cmd/doctor"
	"github.com/homegrew/grew/cmd/extract"
	"github.com/homegrew/grew/cmd/homepage"
	"github.com/homegrew/grew/cmd/info"
	"github.com/homegrew/grew/cmd/install"
	"github.com/homegrew/grew/cmd/leaves"
	"github.com/homegrew/grew/cmd/link"
	"github.com/homegrew/grew/cmd/linkage"
	"github.com/homegrew/grew/cmd/list"
	"github.com/homegrew/grew/cmd/lock"
	"github.com/homegrew/grew/cmd/missing"
	"github.com/homegrew/grew/cmd/pin"
	"github.com/homegrew/grew/cmd/reinstall"
	"github.com/homegrew/grew/cmd/resetupdate"
	"github.com/homegrew/grew/cmd/search"
	"github.com/homegrew/grew/cmd/services"
	"github.com/homegrew/grew/cmd/setup"
	"github.com/homegrew/grew/cmd/shellenv"
	"github.com/homegrew/grew/cmd/sign"
	"github.com/homegrew/grew/cmd/tap"
	cmdtest "github.com/homegrew/grew/cmd/test"
	"github.com/homegrew/grew/cmd/uninstall"
	"github.com/homegrew/grew/cmd/unlink"
	"github.com/homegrew/grew/cmd/unpin"
	"github.com/homegrew/grew/cmd/untap"
	"github.com/homegrew/grew/cmd/update"
	"github.com/homegrew/grew/cmd/upgrade"
	"github.com/homegrew/grew/cmd/uses"
	"github.com/homegrew/grew/cmd/verify"
	"github.com/homegrew/grew/cmd/version"
	"github.com/homegrew/grew/cmd/vulnscan"
	"github.com/homegrew/grew/pkg/flags"
	"github.com/homegrew/grew/pkg/osvdev"
	"github.com/homegrew/grew/pkg/release"
	"github.com/homegrew/grew/pkg/runtime"
	"github.com/spf13/cobra"
)

// SetupTestEnvironment configures API bases and TLS certificates for testing.
// Should only be called when runtime.DevMode is true.
func SetupTestEnvironment() {
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
		if ok := pool.AppendCertsFromPEM(certPEM); !ok {
			fmt.Fprintf(os.Stderr, "failed to parse test cert PEM: no certificates found\n")
			os.Exit(1)
		}

		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: pool}
		http.DefaultTransport = transport
	}
}

// AddCommands attaches all refactored commands to the given cobra.Command.
func AddCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(alias.Command)
	rootCmd.AddCommand(audit.Command)
	rootCmd.AddCommand(autoremove.Command)
	rootCmd.AddCommand(cmdcache.Command)
	rootCmd.AddCommand(cleanup.Command)
	rootCmd.AddCommand(config.Command)
	rootCmd.AddCommand(create.Command)
	rootCmd.AddCommand(deps.Command)
	rootCmd.AddCommand(desc.Command)
	rootCmd.AddCommand(doctor.Command)
	rootCmd.AddCommand(extract.Command)
	rootCmd.AddCommand(homepage.Command)
	rootCmd.AddCommand(info.Command)
	rootCmd.AddCommand(install.Command)
	rootCmd.AddCommand(leaves.Command)
	rootCmd.AddCommand(link.Command)
	rootCmd.AddCommand(linkage.Command)
	rootCmd.AddCommand(list.Command)
	rootCmd.AddCommand(lock.Command)
	rootCmd.AddCommand(missing.Command)
	rootCmd.AddCommand(pin.Command)
	rootCmd.AddCommand(reinstall.Command)
	rootCmd.AddCommand(resetupdate.Command)
	rootCmd.AddCommand(search.Command)
	rootCmd.AddCommand(services.Command)
	rootCmd.AddCommand(setup.Command)
	rootCmd.AddCommand(shellenv.Command)
	rootCmd.AddCommand(sign.Command)
	rootCmd.AddCommand(tap.Command)
	rootCmd.AddCommand(cmdtest.Command)
	rootCmd.AddCommand(uninstall.Command)
	rootCmd.AddCommand(unlink.Command)
	rootCmd.AddCommand(unpin.Command)
	rootCmd.AddCommand(untap.Command)
	rootCmd.AddCommand(update.Command)
	rootCmd.AddCommand(upgrade.Command)
	rootCmd.AddCommand(upgrade.OutdatedCommand)
	rootCmd.AddCommand(uses.Command)
	rootCmd.AddCommand(verify.Command)
	rootCmd.AddCommand(version.Command)
	rootCmd.AddCommand(vulnscan.Command)
}

// InitializeRootCommand sets up global flags and pre-run hooks for the root command.
func InitializeRootCommand(rootCmd *cobra.Command) {
	rootCmd.PersistentFlags().BoolVarP(&flags.Verbose, "verbose", "v", false, "Show detailed output")
	rootCmd.PersistentFlags().BoolVarP(&flags.Debug, "debug", "d", false, "Show debug diagnostics (implies --verbose)")
	rootCmd.PersistentFlags().BoolVarP(&flags.Quiet, "quiet", "q", false, "Only print errors")

	rootCmd.CompletionOptions.DisableDefaultCmd = false
	rootCmd.CompletionOptions.DisableDescriptions = false
	rootCmd.CompletionOptions.DisableNoDescFlag = false
	rootCmd.CompletionOptions.HiddenDefaultCmd = false

	rootCmd.PersistentPreRun = func(c *cobra.Command, args []string) {
		flags.Resolve()
	}
}
