package cmd

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/homegrew/grew/internal/config"
)

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	force := fs.Bool("force", false, "Re-run setup even if already set up")
	fs.BoolVar(force, "f", false, "Re-run setup even if already set up")
	dryRun := fs.Bool("dry-run", false, "Show what would be done without making changes")
	fs.BoolVar(dryRun, "n", false, "Show what would be done without making changes")
	if err := fs.Parse(args); err != nil {
		return err
	}

	isRoot := os.Geteuid() == 0
	var prefix string
	if isRoot {
		prefix = config.SystemPrefix()
	} else {
		prefix = config.UserPrefix()
	}

	// Check if already set up.
	if !*force && !*dryRun && config.IsDir(filepath.Join(prefix, "Cellar")) {
		fmt.Printf("grew is already set up at %s\n", prefix)
		fmt.Println("Run 'grew setup --force' to re-run setup.")
		return nil
	}

	if *dryRun {
		return setupDryRun(prefix, isRoot)
	}

	if isRoot {
		return setupSystem(prefix)
	}
	return setupUser(prefix)
}

// setupDryRun prints what setup would do without making any changes.
func setupDryRun(prefix string, isRoot bool) error {
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
	paths := config.FromRoot(prefix, appDir)

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

	fmt.Printf("==> Setting up grew at %s (system prefix)\n", prefix)
	fmt.Printf("==> Ownership will be transferred to %s\n", u.Username)
	fmt.Println()

	// Create the prefix.
	if err := os.MkdirAll(prefix, 0755); err != nil {
		return fmt.Errorf("create %s: %w", prefix, err)
	}

	// Transfer ownership to the real user.
	fmt.Printf("==> chown -R %s:%s %s\n", u.Username, pg, prefix)
	userGroup := fmt.Sprintf("%s:%s", u.Username, pg)
	cmd := exec.Command("chown", "-R", userGroup, "--", prefix)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chown %s: %w", prefix, err)
	}

	// Create the directory structure.
	return finishSetup(prefix)
}

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func validIdentity(s string) bool {
	return identityPattern.MatchString(s)
}

// setupUser installs grew to ~/.homegrew (no root needed).
func setupUser(prefix string) error {
	fmt.Printf("==> Setting up grew at %s (user prefix)\n", prefix)
	fmt.Println()
	fmt.Println("Tip: run 'sudo grew setup' to install to", config.SystemPrefix(),
		"for better isolation from $HOME.")
	fmt.Println()

	return finishSetup(prefix)
}

const grewRepoURL = "https://github.com/homegrew/grew.git"

func finishSetup(prefix string) error {
	appDir := defaultAppDir()

	paths := config.FromRoot(prefix, appDir)
	fmt.Println("==> Creating directory structure...")
	if err := paths.Init(); err != nil {
		return fmt.Errorf("init directories: %w", err)
	}

	destBin := filepath.Clean(filepath.Join(prefix, "bin", "grew"))

	// Try to install grew from source via git clone + go build.
	// Falls back to copying the running binary if git or go are unavailable.
	repoDir := filepath.Clean(filepath.Join(prefix, "Grew"))
	if err := installFromGit(repoDir, destBin); err != nil {
		Logf("    Note: could not install from source: %v\n", err)
		fmt.Println("==> Falling back to copying current binary")

		exe, exeErr := os.Executable()
		if exeErr != nil {
			return fmt.Errorf("cannot locate current executable: %w", exeErr)
		}
		exe, _ = filepath.EvalSymlinks(exe)
		if exe != destBin {
			if err := copyFile(exe, destBin); err != nil {
				return fmt.Errorf("copy binary to %s: %w", destBin, err)
			}
			fmt.Printf("==> Installed grew binary to %s\n", destBin)
		}
	}

	fmt.Println()
	fmt.Printf("==> grew is ready at %s\n", prefix)
	fmt.Println()
	fmt.Println("Add this to your shell profile:")
	fmt.Println()
	fmt.Printf("  eval \"$(%s/bin/grew shellenv)\"\n", prefix)
	fmt.Println()

	return nil
}

// installFromGit clones the grew repository and builds the binary from source.
// If the repo already exists, it pulls the latest changes instead.
func installFromGit(repoDir, destBin string) error {
	cleanRepoDir := filepath.Clean(repoDir)
	if !filepath.IsAbs(cleanRepoDir) || cleanRepoDir == string(os.PathSeparator) {
		return fmt.Errorf("invalid repository directory: %s", repoDir)
	}
	if cleanRepoDir != repoDir {
		// Avoid using a path that relies on traversal or redundant components.
		return fmt.Errorf("repository directory must not contain path traversal elements: %s", repoDir)
	}

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
		fmt.Println("==> Updating grew source...")
		pull := exec.Command(gitPath, "pull", "--ff-only")
		pull.Dir = cleanRepoDir
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		if err := pull.Run(); err != nil {
			return fmt.Errorf("git pull: %w", err)
		}
	} else {
		// Clone fresh.
		fmt.Printf("==> Cloning grew from %s\n", grewRepoURL)
		clone := exec.Command(gitPath, "clone", "--depth", "1", "--", grewRepoURL, cleanRepoDir)
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
	}

	// Generate version and build.
	fmt.Println("==> Building grew from source...")
	generate := exec.Command(goPath, "generate", "./internal/...")
	generate.Dir = cleanRepoDir
	generate.Stdout = os.Stdout
	generate.Stderr = os.Stderr
	if err := generate.Run(); err != nil {
		Logf("    Warning: go generate failed: %v\n", err)
	}

	build := exec.Command(goPath, "build", "-o", destBin, ".")
	build.Dir = cleanRepoDir
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	fmt.Printf("==> Built and installed grew to %s\n", destBin)
	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Normalize and validate the destination path to reduce the risk of
	// path traversal when dst is derived from untrusted input.
	dstClean := filepath.Clean(dst)
	baseDir := filepath.Dir(dstClean)
	baseDir = filepath.Clean(baseDir)
	if rel, err := filepath.Rel(baseDir, dstClean); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("invalid destination path %q", dst)
	}

	dstFile, err := os.OpenFile(dstClean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		// Ensure srcFile is closed before returning on error.
		_ = srcFile.Close()
		return err
	}

	_, err = io.Copy(dstFile, srcFile)
	if cerr := dstFile.Close(); err == nil {
		err = cerr
	}
	return err
}

// defaultAppDir returns the appropriate Applications directory.
// Under sudo (system install), casks go to /Applications.
// Otherwise, they go to ~/Applications.
func defaultAppDir() string {
	// HOMEGREW_APPDIR, if set, overrides the default applications directory.
	// It should be a path to the directory where grew-managed apps are installed.
	// Both relative and absolute paths are accepted; relative paths are resolved
	// to an absolute, cleaned path. If the value cannot be resolved, it is ignored.
	if v := os.Getenv("HOMEGREW_APPDIR"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return filepath.Clean(abs)
		}
		// If the override cannot be resolved to an absolute path,
		// ignore it and fall back to the default locations below.
	}
	if os.Geteuid() == 0 {
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

func primaryGroup(u *user.User) string {
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return u.Gid
	}
	return g.Name
}
