package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/auditlog"
	"github.com/homegrew/grew/internal/cache"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/context"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/flags"
	"github.com/homegrew/grew/internal/fsutil"
	"github.com/homegrew/grew/internal/linker"
	"github.com/homegrew/grew/internal/osvdev"
	"github.com/homegrew/grew/internal/release"
	"github.com/homegrew/grew/internal/runtime"
	"github.com/homegrew/grew/internal/version"
	"github.com/homegrew/grew/pkg/safepath"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
        Use:   "grew",
        Short: "A package manager written in Go",
        Long:  `grew is a high-performance, Go-based command-line package manager inspired by Homebrew.`,
        Version: version.Version(),
        PersistentPreRun: func(cmd *cobra.Command, args []string) {
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

	rootCmd.PersistentFlags().BoolVarP(&flags.Verbose, "verbose", "v", false, "Show detailed output")
	rootCmd.PersistentFlags().BoolVarP(&flags.Debug, "debug", "d", false, "Show debug diagnostics (implies --verbose)")
	rootCmd.PersistentFlags().BoolVarP(&flags.Quiet, "quiet", "q", false, "Only print errors")
}

// Run executes the root command.
func Run(args []string) error {
	rootCmd.Version = version.Version()
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

// ... rest of context definitions ...

// readContext bundles objects needed by read-only commands (info, search, outdated, deps).
type readContext = *context.Context

// newReadContext initialises paths and the core tap for read-only commands.
func newReadContext() (readContext, error) {
	return context.New()
}

// installContext bundles the common objects used by install, reinstall, and upgrade.
type installContext struct {
	readContext
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
	common, err := context.New()
	if err != nil {
		return nil, err
	}

	if err := safepath.SafeAbsolutePath(common.Paths.Tmp); err != nil {
		return nil, fmt.Errorf("invalid temporary directory %q: %w", common.Paths.Tmp, err)
	}

	lock, err := acquireGlobalLock(common.Paths)
	if err != nil {
		return nil, err
	}

	return &installContext{
		readContext: common,
		Linker:      &linker.Linker{Paths: common.Paths},
		DL:          &downloader.Downloader{TmpDir: common.Paths.Tmp, Cache: cache.New(common.Paths.Cache)},
		AuditLog:    auditlog.New(common.Paths.Log),
		GlobalLock:  lock,
	}, nil
}

func acquireGlobalLock(paths config.Paths) (*os.File, error) {
        if err := safepath.SafeAbsolutePath(paths.Root); err != nil {
                return nil, fmt.Errorf("invalid root directory %q: %w", paths.Root, err)
        }
        rootAbs, err := filepath.Abs(paths.Root)
        if err != nil {
                return nil, fmt.Errorf("resolve root directory: %w", err)
        }
        rootAbs = filepath.Clean(rootAbs)

        lockPath := filepath.Join(rootAbs, ".grew.lock")
        lockAbs, err := filepath.Abs(lockPath)
        if err != nil {
                return nil, fmt.Errorf("resolve lock file path: %w", err)
        }
        lockAbs = filepath.Clean(lockAbs)

        rel, err := filepath.Rel(rootAbs, lockAbs)
        if err != nil {
                return nil, fmt.Errorf("validate lock file path: %w", err)
        }
        if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
                return nil, fmt.Errorf("invalid lock file path %q: escapes root %q", lockAbs, rootAbs)
        }

        f, err := os.OpenFile(lockAbs, os.O_CREATE|os.O_RDWR, 0600)
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
