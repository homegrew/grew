package cmd

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

	"github.com/homegrew/grew/internal/config"
	grewrt "github.com/homegrew/grew/internal/runtime"
	pathutil "github.com/homegrew/grew/pkg/safepath"
	"github.com/spf13/cobra"
)

var (
	setupForce  bool
	setupDryRun bool
	setupUnsafe bool
)

var SetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "One-time setup of the grew prefix",
	Long: `Set up the grew directory structure. Requires root (sudo).

System prefix locations:
  macOS (Apple Silicon): /opt/homegrew
  macOS (Intel):         /usr/local/homegrew
  Linux:                 /usr/local/homegrew

The command:
  1. Creates the system prefix directory
  2. Transfers ownership to SUDO_USER (no root needed at runtime)
  3. Creates the internal directory structure
  4. Copies the grew binary into <prefix>/bin/

Path inference: grew infers its prefix from the binary location. If
the binary is at <prefix>/bin/grew, all paths are derived from <prefix>
automatically — no HOMEGREW_PREFIX env var needed.

Security: a system prefix isolates builds from $HOME, preventing
sandboxed formulas from accessing ~/.ssh, ~/.gnupg, etc.

Developer mode: builds compiled with -tags devmode can install to
~/.homegrew without root by passing --unsafe:
  grew setup --unsafe

After setup, add to your shell profile:
  eval "$(grew shellenv)"

Examples:
  sudo grew setup
  sudo grew setup --dry-run
  sudo grew setup --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Debug("starting setup command execution")

		// Set the unsafe flag on the runtime before Init so it can allow
		// non-root operation in devmode builds.
		grewrt.Unsafe = setupUnsafe

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

		if isRoot {
			return setupSystem(prefix)
		}
		return setupUser(prefix)
	},
}

func init() {
	SetupCmd.Flags().BoolVarP(&setupForce, "force", "f", false, "Re-run setup even if already set up")
	SetupCmd.Flags().BoolVarP(&setupDryRun, "dry-run", "n", false, "Show what would be done without making changes")
	SetupCmd.Flags().BoolVar(&setupUnsafe, "unsafe", false, "Allow user-local install without root (devmode builds only)")
	rootCmd.AddCommand(SetupCmd)
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

	repoDir := filepath.Clean(filepath.Join(prefix, "Grew"))
	destBin := filepath.Clean(filepath.Join(prefix, "bin", "grew"))
	_, errGit := exec.LookPath("git")
	_, errGo := exec.LookPath("go")
	gitAvailable := errGit == nil
	goAvailable := errGo == nil

	fmt.Println()
	if gitAvailable && goAvailable {
		fmt.Printf("[dry-run] Clone grew repo: %s -> %s\n", grewRepoURL, repoDir)
		fmt.Printf("[dry-run] Build from source: go build -o %s\n", destBin)
	} else {
		exe, err := os.Executable()
		if err == nil {
			exe, _ = filepath.EvalSymlinks(exe)
			fmt.Printf("[dry-run] Fallback: copy binary %s -> %s\n", exe, destBin)
		}
		if !gitAvailable {
			fmt.Println("[dry-run]   (git not found — cannot clone)")
		}
		if !goAvailable {
			fmt.Println("[dry-run]   (go not found — cannot build from source)")
		}
	}

	fmt.Println()
	fmt.Println("[dry-run] After setup, add to your shell profile:")
	fmt.Printf("[dry-run]   eval \"$(%s/bin/grew shellenv)\"\n", prefix)

	return nil
}

// setupSystem installs grew to the system prefix (requires root).
// After creating the directory, ownership is transferred to SUDO_USER
// so all subsequent operations are rootless.
func setupSystem(prefix string) error {
	// Determine the real (non-root) user who ran sudo.
	realUser := os.Getenv("SUDO_USER")
	if realUser == "" {
		return fmt.Errorf("could not determine the real user; run with: sudo grew setup")
	}
	u, err := user.Lookup(realUser)
	if err != nil {
		return fmt.Errorf("lookup user %s: %w", realUser, err)
	}

	pg := primaryGroup(u)
	if !validIdentity(u.Username) || !validIdentity(pg) {
		return fmt.Errorf("invalid username or group name: %q, %q (must contain only letters, digits, underscore, dot, or hyphen)", u.Username, pg)
	}

	fmt.Fprintf(os.Stderr, "==> Setting up grew at %s (system prefix)\n", prefix)
	fmt.Fprintf(os.Stderr, "==> Ownership will be transferred to %s\n", u.Username)
	fmt.Println()

	// Create the prefix.
	if err := os.MkdirAll(prefix, 0755); err != nil {
		return fmt.Errorf("create %s: %w", prefix, err)
	}

	// Transfer ownership to the real user.

	userGroup := strings.Join([]string{u.Username, pg}, ":")
	fmt.Fprintf(os.Stderr, "==> chown -R %s %s\n", userGroup, prefix)
	slog.Info(fmt.Sprintf("chown -R %s %s", userGroup, prefix))

	chownExe, err := exec.LookPath("chown")
	if err != nil {
		return fmt.Errorf("chown not found in PATH: %w", err)
	}

	cmd := exec.Command(chownExe, "-R", "--", userGroup, prefix)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	slog.Debug(fmt.Sprintf("running command: %s", cmd.String()))

	if chownErr := cmd.Run(); chownErr != nil {
		if pathError, ok := errors.AsType[*os.PathError](chownErr); ok {
			return fmt.Errorf("could not chown %s: %w", pathError.Path, chownErr)
		} else if exitError, ok := errors.AsType[*exec.ExitError](chownErr); ok {
			return fmt.Errorf("chown exited with code %d: %w", exitError.ExitCode(), chownErr)
		}
		return fmt.Errorf("could not chown %s: %w", userGroup, chownErr)
	}

	// Create the directory structure and install the binary.
	if err := finishSetup(prefix); err != nil {
		return err
	}

	// Re-apply ownership after finishSetup, which may have created new
	// files as root. This ensures the entire prefix is owned by the real
	// user, which is critical for --force re-runs where previous files
	// may have wrong permissions.
	fmt.Fprintf(os.Stderr, "==> Fixing permissions: chown -R %s %s\n", userGroup, prefix)
	fixCmd := exec.Command(chownExe, "-R", "--", userGroup, prefix)
	fixCmd.Stdout = os.Stdout
	fixCmd.Stderr = os.Stderr
	if chownErr := fixCmd.Run(); chownErr != nil {
		return fmt.Errorf("fix permissions: %w", chownErr)
	}

	return nil
}

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func validIdentity(s string) bool {
	return identityPattern.MatchString(s)
}

// setupUser installs grew to ~/.homegrew (devmode only, no root needed).
func setupUser(prefix string) error {
	fmt.Fprintf(os.Stderr, "==> Setting up grew at %s (user prefix, devmode)\n", prefix)
	fmt.Println()
	fmt.Println("Tip: run 'sudo grew setup' to install to", grewrt.SystemPrefix(),
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

	// Try to install grew from source via git clone + go build.
	// Falls back to copying the running binary if git or go are unavailable.
	repoDir := filepath.Clean(filepath.Join(prefix, "Grew"))
	if err := installFromGit(repoDir, destBin, true); err != nil {
		slog.Info(fmt.Sprintf("note: could not install from source: %v", err))
		fmt.Fprintln(os.Stderr, "==> Falling back to copying current binary")

		exe, exeErr := os.Executable()
		if exeErr != nil {
			return fmt.Errorf("cannot locate current executable: %w", exeErr)
		}
		resolved, evalErr := filepath.EvalSymlinks(exe)
		if evalErr != nil {
			return fmt.Errorf("resolve executable symlinks: %w", evalErr)
		}
		exe = resolved
		if exe != destBin {
			if err := copyFile(exe, destBin); err != nil {
				return fmt.Errorf("copy binary to %s: %w", destBin, err)
			}
			fmt.Fprintf(os.Stderr, "==> Installed grew binary to %s\n", destBin)
		}
	}

	fmt.Println()
	fmt.Fprintf(os.Stderr, "==> grew is ready at %s\n", prefix)
	fmt.Println()
	fmt.Println("Add this to your shell profile:")
	fmt.Println()
	fmt.Printf("  eval \"$(%s/bin/grew shellenv)\"\n", prefix)
	fmt.Println()

	return nil
}

var ErrNoGitRepo = errors.New("no git repository found")

// installFromGit clones the grew repository and builds the binary from source.
// If the repo already exists, it pulls the latest changes instead.
func installFromGit(repoDir, destBin string, allowClone bool) error {
	if err := pathutil.SafeAbsolutePath(repoDir); err != nil {
		return fmt.Errorf("invalid repository directory: %w", err)
	}
	if err := pathutil.SafeAbsolutePath(destBin); err != nil {
		return fmt.Errorf("invalid destination binary path: %w", err)
	}

	cleanRepoDir := repoDir // already validated as clean

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git not found in PATH")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("go not found in PATH")
	}

	gitDir := filepath.Clean(filepath.Join(cleanRepoDir, ".git"))
	if _, err := os.Stat(gitDir); err == nil {
		// Repo exists — pull latest.
		fmt.Fprintln(os.Stderr, "==> Updating grew source...")
		pull := exec.Command(gitPath, "pull", "--ff-only")
		pull.Dir = cleanRepoDir
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		if err := pull.Run(); err != nil {
			return fmt.Errorf("git pull: %w", err)
		}
	} else {
		if !allowClone {
			return ErrNoGitRepo
		}
		// Clone fresh.
		fmt.Fprintf(os.Stderr, "==> Cloning grew from %s\n", grewRepoURL)
		clone := exec.Command(gitPath, "clone", "--depth", "1", "--", grewRepoURL, cleanRepoDir)
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
	}

	// Generate version and build.
	fmt.Fprintln(os.Stderr, "==> Building grew from source...")
	generate := exec.Command(goPath, "generate", "./internal/...")
	generate.Dir = cleanRepoDir
	generate.Stdout = os.Stdout
	generate.Stderr = os.Stderr
	if err := generate.Run(); err != nil {
		slog.Warn(fmt.Sprintf("go generate failed: %v", err))
	}

	build := exec.Command(goPath, "build", "-o", destBin, ".")
	build.Dir = cleanRepoDir
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	fmt.Fprintf(os.Stderr, "==> Built and installed grew to %s\n", destBin)
	return nil
}

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
