package setup

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/homegrew/grew/internal/cache"
	"github.com/homegrew/grew/internal/installer"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/downloader"
	"github.com/homegrew/grew/internal/release"
	grewrt "github.com/homegrew/grew/internal/runtime"
	"github.com/homegrew/grew/internal/sudo"
	pathutil "github.com/homegrew/grew/pkg/safepath"
	"github.com/homegrew/grew/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	setupForce  bool
	setupDryRun bool
)

var Command = &cobra.Command{
	Use:   "setup",
	Short: "One-time setup of the grew prefix",
	Long: `Set up the grew directory structure.

System prefix locations:
  macOS (Apple Silicon): /opt/homegrew
  macOS (Intel):         /usr/local/homegrew

The command:
  1. Creates the system prefix directory
  2. Transfers ownership to the current user (no root needed at runtime)
  3. Creates the internal directory structure
  4. Downloads and installs the grew binary into <prefix>/bin/

Path inference: grew infers its prefix from the binary location. If
the binary is at <prefix>/bin/grew, all paths are derived from <prefix>
automatically — no HOMEGREW_PREFIX env var needed.

Security: a system prefix isolates builds from $HOME, preventing
sandboxed formulas from accessing ~/.ssh, ~/.gnupg, etc.

Examples:
  grew setup
  grew setup --dry-run
  grew setup --force`,
	RunE: func(c *cobra.Command, args []string) error {
		slog.Debug("starting setup command execution")

		if err := grewrt.Init(); err != nil {
			return fmt.Errorf("initializing runtime environment: %w", err)
		}

		env := grewrt.Env()
		prefix := env.DefaultPrefix()
		isRoot := env.RunAsRoot()

		// Check if already set up.
		if !setupForce && !setupDryRun && config.IsDir(filepath.Join(prefix, "Cellar")) {
			fmt.Printf("grew is already set up at %s\n", prefix)
			fmt.Println("Run 'grew setup --force' to re-run setup.")
			return nil
		}

		if setupDryRun {
			return runSetupDryRun(prefix, isRoot)
		}

		// If we are installing to the recommended system prefix, use setupSystem
		// which handles ownership transfer (and elevation if needed).
		if prefix == grewrt.SystemPrefix() {
			return setupSystem(prefix)
		}
		
		// Otherwise (e.g. user prefix), just finish setup.
		return setupUser(prefix)
	},
}

func init() {
	Command.Flags().BoolVarP(&setupForce, "force", "f", false, "Re-run setup even if already set up")
	Command.Flags().BoolVarP(&setupDryRun, "dry-run", "n", false, "Show what would be done without making changes")
}

// runSetupDryRun prints what setup would do without making any changes.
func runSetupDryRun(prefix string, isRoot bool) error {
	fmt.Println("[dry-run] No changes will be made.")
	fmt.Println()

	if isRoot {
		realUser := os.Getenv("SUDO_USER")
		if realUser == "" {
			realUser = "(unknown)"
		}
		fmt.Printf("[dry-run] Mode: system prefix (running as root)\n")
		fmt.Printf("[dry-run] Prefix: %s\n", prefix)
		fmt.Printf("[dry-run] mkdir -p %s\n", prefix)
		u, err := user.Lookup(realUser)
		if err == nil {
			fmt.Printf("[dry-run] chown -R %s:%s %s\n", u.Username, primaryGroup(u), prefix)
		} else {
			fmt.Printf("[dry-run] chown -R %s %s\n", realUser, prefix)
		}
	} else {
		fmt.Printf("[dry-run] Mode: user prefix (no root)\n")
		fmt.Printf("[dry-run] Prefix: %s\n", prefix)
	}

	appDir := defaultAppDir()
	cacheDir := defaultCacheDir()
	paths := config.FromRoot(prefix, appDir, cacheDir)

	fmt.Println()
	fmt.Println("[dry-run] Directories to create:")
	for _, dir := range []struct{ name, path string }{
		{"Root", paths.Root},
		{"Cellar", paths.Cellar},
		{"opt", paths.Opt},
		{"bin", paths.Bin},
		{"lib", paths.Lib},
		{"include", paths.Include},
		{"Taps", paths.Taps},
		{"CoreTap", paths.CoreTap},
		{"CaskTap", paths.CaskTap},
		{"Caskroom", paths.Caskroom},
		{"AppDir", paths.AppDir},
		{"Cache", paths.Cache},
		{"tmp", paths.Tmp},
	} {
		status := "create"
		if config.IsDir(dir.path) {
			status = "exists"
		}
		fmt.Printf("[dry-run]   %-10s %s (%s)\n", dir.name, dir.path, status)
	}

	destBin := filepath.Clean(filepath.Join(prefix, "bin", "grew"))

	fmt.Println()
	fmt.Printf("[dry-run] Download latest release and install binary to %s\n", destBin)

	fmt.Println()
	fmt.Println("[dry-run] After setup, add to your shell profile:")
	fmt.Printf("[dry-run]   eval \"$(%s/bin/grew shellenv)\"\n", prefix)

	return nil
}

// setupSystem installs grew to the system prefix (requires root).
// After creating the directory, ownership is transferred to SUDO_USER
// so all subsequent operations are rootless.
func setupSystem(prefix string) error {
	// Determine the target user who will own the prefix.
	var u *user.User
	var err error
	if realUser := os.Getenv("SUDO_USER"); realUser != "" {
		u, err = user.Lookup(realUser)
	} else {
		u, err = user.Current()
	}
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}

	pg := primaryGroup(u)
	if !validIdentity(u.Username) || !validIdentity(pg) {
		return fmt.Errorf("invalid username or group name: %q, %q (must contain only letters, digits, underscore, dot, or hyphen)", u.Username, pg)
	}

	userGroup := strings.Join([]string{u.Username, pg}, ":")
	ui.FprintArrow(os.Stderr, "Setting up grew at %s (system prefix)", prefix)
	ui.FprintArrow(os.Stderr, "Ownership will be transferred to %s", u.Username)
	fmt.Println()

	isRoot := os.Geteuid() == 0

	if !isRoot {
		// If we are not root, we need to elevate to create the prefix
		// and transfer its ownership to the current user.
		script := fmt.Sprintf("mkdir -p %q && chown -R %q %q", prefix, userGroup, prefix)
		slog.Info("Elevating privileges to create prefix and transfer ownership")
		if err := sudo.RunSudoCmd("sh", "-c", script); err != nil {
			return fmt.Errorf("elevation failed: %w", err)
		}
	} else {
		// Already running as root (e.g. legacy start via sudo).
		if err := os.MkdirAll(prefix, 0755); err != nil {
			return fmt.Errorf("create %s: %w", prefix, err)
		}

		chownExe, err := exec.LookPath("chown")
		if err != nil {
			return fmt.Errorf("chown not found in PATH: %w", err)
		}

		cmd := exec.Command(chownExe, "-R", "--", userGroup, prefix)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if chownErr := cmd.Run(); chownErr != nil {
			return fmt.Errorf("chown failed: %w", chownErr)
		}
	}

	// Create the directory structure and install the binary.
	// This will now run as the normal user if we were not root,
	// or as root if we were.
	if err := finishSetup(prefix); err != nil {
		return err
	}

	// If we are root, we must re-apply ownership to the entire prefix,
	// because finishSetup just created files owned by root.
	if isRoot {
		ui.FprintArrow(os.Stderr, "Fixing permissions: chown -R %s %s", userGroup, prefix)
		chownExe, _ := exec.LookPath("chown")
		fixCmd := exec.Command(chownExe, "-R", "--", userGroup, prefix)
		fixCmd.Stdout = os.Stdout
		fixCmd.Stderr = os.Stderr
		if chownErr := fixCmd.Run(); chownErr != nil {
			return fmt.Errorf("fix permissions: %w", chownErr)
		}
	}

	return nil
}

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func validIdentity(s string) bool {
	return identityPattern.MatchString(s)
}

// setupUser installs grew to ~/.homegrew.
func setupUser(prefix string) error {
	ui.FprintArrow(os.Stderr, "Setting up grew at %s (user prefix)", prefix)
	fmt.Println()
	fmt.Println("Tip: run 'grew setup' to install to", grewrt.SystemPrefix(),
		"for better isolation from $HOME.")
	fmt.Println()

	return finishSetup(prefix)
}

const grewRepoURL = "https://github.com/homegrew/grew.git"

func finishSetup(prefix string) error {
	appDir := defaultAppDir()
	cacheDir := defaultCacheDir()

	paths := config.FromRoot(prefix, appDir, cacheDir)
	fmt.Fprintln(os.Stderr, "==> Creating directory structure...")
	if err := paths.Init(); err != nil {
		return fmt.Errorf("init directories: %w", err)
	}

	// Remove unsupported share directories if they exist from a prior run
	_ = os.RemoveAll(filepath.Join(paths.Share, "man"))
	_ = os.RemoveAll(filepath.Join(paths.Share, "info"))
	_ = os.Remove(paths.Share) // only removes if empty

	destBin := filepath.Clean(filepath.Join(prefix, "bin", "grew"))

	// Mandatory binary install.
	if err := installBinary(destBin, prefix); err != nil {
		return fmt.Errorf("install binary release: %w", err)
	}

	fmt.Println()
	ui.FprintArrow(os.Stderr, "grew is ready at %s", prefix)
	fmt.Println()
	fmt.Println("Add this to your shell profile:")
	fmt.Println()
	fmt.Printf("  eval \"$(%s/bin/grew shellenv)\"\n", prefix)
	fmt.Println()

	return nil
}

func installBinary(destBin, prefix string) error {
	ui.FprintArrow(os.Stderr, "Downloading latest grew release...")
	rel, err := release.FetchLatest()
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}

	paths := config.FromRoot(prefix, defaultAppDir(), defaultCacheDir())
	rel.DL = &downloader.Downloader{
		TmpDir: paths.Tmp,
		Cache:  cache.New(paths.Cache),
	}

	if err := installer.InstallLatestRelease(destBin, rel); err != nil {
		return err
	}

	return nil
}

var ErrNoGitRepo = errors.New("no git repository found")

func copyFile(src, dst string) error {
	if err := pathutil.SafeAbsolutePath(dst); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	_, err = io.Copy(dstFile, srcFile)
	if cerr := dstFile.Close(); err == nil {
		err = cerr
	}
	return err
}

// defaultAppDir returns the appropriate Applications directory.
// HOMEGREW_APPDIR overrides the default. Under root, /Applications is used;
// in devmode user-local installs, ~/Applications is used.
func defaultAppDir() string {
	if v := os.Getenv("HOMEGREW_APPDIR"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return filepath.Clean(abs)
		}
	}
	if grewrt.Env().RunAsRoot() {
		return "/Applications"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	appDir := filepath.Join(home, "Applications")
	if abs, err := filepath.Abs(appDir); err == nil {
		return filepath.Clean(abs)
	}
	return appDir
}

// defaultCacheDir returns the appropriate cache directory.
// HOMEGREW_CACHE overrides the default.
func defaultCacheDir() string {
	if v := os.Getenv("HOMEGREW_CACHE"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return filepath.Clean(abs)
		}
	}
	if ucd, err := os.UserCacheDir(); err == nil {
		return filepath.Join(ucd, "Homegrew")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cache", "homegrew")
}

func primaryGroup(u *user.User) string {
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return u.Gid
	}
	return g.Name
}
