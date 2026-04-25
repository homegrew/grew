package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/osvdev"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/runtime"
	"github.com/homegrew/grew/internal/tap"
	"github.com/homegrew/grew/internal/version"
	"github.com/homegrew/grew/pkg/safepath"
)

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
}

func Run(args []string) error {
	// Strip global flags (verbose, debug) before dispatch — they can
	// appear anywhere on the command line.
	args = flags.Parse(args)

	// Handle --version separately (it was not consumed by flags.Parse).
	var filtered []string
	for _, a := range args {
		if a == "--version" {
			fmt.Printf("grew %s\n", version.Version())
			return nil
		}
		filtered = append(filtered, a)
	}
	args = filtered

	// Apply flag implications and configure the logger.
	flags.Resolve()

	if len(args) == 0 {
		printUsage()
		return nil
	}

	// Handle "grew --help" and "grew -h"
	if args[0] == "--help" || args[0] == "-h" {
		return runHelp(args[1:])
	}

	commands := map[string]func([]string) error{
		"install":      RunInstall,
		"uninstall":    runUninstall,
		"remove":       runUninstall,
		"list":         runList,
		"info":         runInfo,
		"search":       runSearch,
		"link":         runLink,
		"unlink":       runUnlink,
		"update":       runUpdate,
		"reset-update": runResetUpdate,
		"upgrade":      runUpgrade,
		"outdated":     runOutdated,
		"reinstall":    runReinstall,
		"cleanup":      runCleanup,
		"deps":         runDeps,
		"alias":        runAlias,
		"audit":        runAudit,
		"doctor":       runDoctor,
		"dr":           runDoctor,
		"config":       runConfig,
		"shellenv":     runShellenv,
		"services":     runServices,
		"setup":        runSetup,
		"verify":       runVerify,
		"lock":         runLock,
		"sign":         runSign,
		"pin":          runPin,
		"unpin":        runUnpin,
		"linkage":      runLinkage,
		"vuln-scan":    runVulnScan,
		"completion":   runCompletion,
		"_extract":     runExtract, // internal: sandboxed extraction subprocess
		"help":         runHelp,
	}

	handler, ok := commands[args[0]]
	if !ok {
		// Check aliases
		a, err := loadAliases()
		if err != nil {
			slog.Debug(fmt.Sprintf("failed to load aliases: %v", err))
		} else if target, exists := a[args[0]]; exists {
			expanded := append([]string{target}, args[1:]...)
			if h, found := commands[expanded[0]]; found {
				return h(expanded[1:])
			}
		}
		return fmt.Errorf("unknown command: %s\nRun 'grew' for usage", args[0])
	}
	return handler(args[1:])
}

// readContext bundles objects needed by read-only commands (info, search, outdated, deps).
type readContext struct {
	Paths  config.Paths
	Loader *formula.Loader
	Cellar *cellar.Cellar
}

// newReadContext initialises paths and the core tap for read-only commands.
func newReadContext() (*readContext, error) {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return nil, err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		return nil, fmt.Errorf("init core tap: %w", err)
	}

	return &readContext{
		Paths:  paths,
		Loader: newLoader(paths.Taps),
		Cellar: &cellar.Cellar{Path: paths.Cellar},
	}, nil
}

// installContext bundles the common objects used by install, reinstall, and upgrade.
type installContext struct {
	Paths      config.Paths
	Loader     *formula.Loader
	Cellar     *cellar.Cellar
	Linker     *linker.Linker
	DL         *downloader.Downloader
	AuditLog   *auditlog.Logger
	GlobalLock *os.File
}

func (c *installContext) Close() {
	if c.GlobalLock != nil {
		fsutil.Unlock(c.GlobalLock)
		if err := c.GlobalLock.Close(); err != nil {
			slog.Warn("close global lock", "error", err)
		}
		c.GlobalLock = nil
	}
}

// newInstallContext initialises paths, the core tap, and returns the shared context.
func newInstallContext() (*installContext, error) {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return nil, err
	}
	if err := safepath.SafeAbsolutePath(paths.Tmp); err != nil {
		return nil, fmt.Errorf("invalid temporary directory %q: %w", paths.Tmp, err)
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		return nil, fmt.Errorf("init core tap: %w", err)
	}

	lock, err := acquireGlobalLock(paths)
	if err != nil {
		return nil, err
	}

	return &installContext{
		Paths:      paths,
		Loader:     newLoader(paths.Taps),
		Cellar:     &cellar.Cellar{Path: paths.Cellar},
		Linker:     &linker.Linker{Paths: paths},
		DL:         &downloader.Downloader{TmpDir: paths.Tmp},
		AuditLog:   auditlog.New(paths.Log),
		GlobalLock: lock,
	}, nil
}

func acquireGlobalLock(paths config.Paths) (*os.File, error) {
	lockPath := filepath.Join(paths.Root, ".grew.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := fsutil.Lock(f); err != nil {
		if cerr := f.Close(); cerr != nil {
			return nil, fmt.Errorf("acquire global lock: %w (also failed to close lock file: %v)", err, cerr)
		}
		return nil, fmt.Errorf("acquire global lock: %w", err)
	}
	return f, nil
}

// newLoader creates a formula.Loader with debug logging wired in.
func newLoader(tapDir string) *formula.Loader {
	l := formula.NewLoader(tapDir)
	l.DebugLog = func(format string, args ...any) {
		slog.Debug(fmt.Sprintf(format, args...))
	}
	return l
}

func printUsage() {
	fmt.Print(`grew - a package manager written in Go

Usage:
  grew [flags] <command> [arguments]

Flags:
  -v, --verbose        Show detailed output
  -d, --debug          Show debug diagnostics (implies --verbose)
      --version        Print version and exit

Commands:
  install [-s] [-n] [--skip-post-install] [--skip-link] <formula>  Install a formula (use --cask for apps, -s for source build, -n for dry run)
  uninstall <formula>  Uninstall a formula or cask (--cask)
  list [-1] [-l] [-t] [--versions] [--multiple] List installed formulas or casks (--cask)
  info <formula>       Show formula or cask info (--cask)
  search <query>       Search formulas or casks (--cask)
  link <formula>       Create symlinks for a formula
  unlink <formula>     Remove symlinks for a formula
  update               Update formula definitions
  reset-update         Wipe and re-fetch all tap definitions
  reinstall [-f] [--zap] [-s] <formula>  Reinstall a formula (-f force, --zap deep clean, -s from source)
  upgrade [formula]    Upgrade outdated packages (or a specific one)
  outdated             List packages with newer versions available
  cleanup [-n]         Remove old versions and temp files (-n for dry run)
  deps [flags] <formula>  Show dependencies for a formula
  audit [formula]      Audit formula/cask definitions for problems
  alias [subcommand]   Manage command aliases
  services [sub]       Manage background services (start, stop, list, ...)
  setup                One-time setup of the grew prefix
  doctor               Check for common problems
  config               Show grew and system configuration
  shellenv [shell]     Print shell environment setup
  verify [formula]     Verify installed package integrity
  linkage [--test] [--strict] [--reverse] [--cached] [-q] <formula>  Check dynamic library dependencies
  vuln-scan [formula]  Scan installed packages for security vulnerabilities
  lock [subcommand]    Manage the formula lockfile (generate, check, show)
  sign <formula> <key> Sign formula SHA256 hashes with an Ed25519 key
  pin <formula>        Pin a formula to prevent upgrades
  unpin <formula>      Unpin a formula to allow upgrades
  help [command]       Show help for a command
`)
}
