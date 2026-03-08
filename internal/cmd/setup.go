package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/homegrew/grew/internal/config"
)

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	force := fs.Bool("force", false, "Re-run setup even if already set up")
	fs.BoolVar(force, "f", false, "Re-run setup even if already set up")
	dryRun := fs.Bool("dry-run", false, "Show what would be done without making changes")
	fs.BoolVar(dryRun, "s", false, "Show what would be done without making changes")
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

	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		destBin := filepath.Join(prefix, "bin", "grew")
		if exe != destBin {
			fmt.Printf("\n[dry-run] Copy binary: %s -> %s\n", exe, destBin)
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

	fmt.Printf("==> Setting up grew at %s (system prefix)\n", prefix)
	fmt.Printf("==> Ownership will be transferred to %s\n", u.Username)
	fmt.Println()

	// Create the prefix.
	if err := os.MkdirAll(prefix, 0755); err != nil {
		return fmt.Errorf("create %s: %w", prefix, err)
	}

	// Transfer ownership to the real user.
	fmt.Printf("==> chown -R %s:%s %s\n", u.Username, primaryGroup(u), prefix)
	cmd := exec.Command("chown", "-R", "--", u.Username+":"+primaryGroup(u), prefix)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chown %s: %w", prefix, err)
	}

	// Create the directory structure.
	return finishSetup(prefix)
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

func finishSetup(prefix string) error {
	appDir := defaultAppDir()

	paths := config.FromRoot(prefix, appDir)
	fmt.Println("==> Creating directory structure...")
	if err := paths.Init(); err != nil {
		return fmt.Errorf("init directories: %w", err)
	}

	// Copy the current binary into <prefix>/bin/grew so path inference works.
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		destBin := filepath.Join(prefix, "bin", "grew")
		if exe != destBin {
			if err := copyFile(exe, destBin); err != nil {
				Logf("    Note: could not copy binary to %s: %v\n", destBin, err)
			} else {
				fmt.Printf("==> Installed grew binary to %s\n", destBin)
			}
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

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}

// defaultAppDir returns the appropriate Applications directory.
// Under sudo (system install), casks go to /Applications.
// Otherwise, they go to ~/Applications.
func defaultAppDir() string {
	if v := os.Getenv("HOMEGREW_APPDIR"); v != "" {
		return v
	}
	if os.Geteuid() == 0 {
		return "/Applications"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, "Applications")
}

func primaryGroup(u *user.User) string {
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return u.Gid
	}
	return g.Name
}
