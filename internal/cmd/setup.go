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
	if !*force && config.IsDir(filepath.Join(prefix, "Cellar")) {
		fmt.Printf("grew is already set up at %s\n", prefix)
		fmt.Println("Run 'grew setup --force' to re-run setup.")
		return nil
	}

	if isRoot {
		return setupSystem(prefix)
	}
	return setupUser(prefix)
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
	cmd := exec.Command("chown", "-R", u.Username+":"+primaryGroup(u), prefix)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chown %s: %w", prefix, err)
	}

	// Create the directory structure.
	return finishSetup(prefix)
}

// setupUser installs grew to ~/.grew (no root needed).
func setupUser(prefix string) error {
	fmt.Printf("==> Setting up grew at %s (user prefix)\n", prefix)
	fmt.Println()
	fmt.Println("Tip: run 'sudo grew setup' to install to", config.SystemPrefix(),
		"for better isolation from $HOME.")
	fmt.Println()

	return finishSetup(prefix)
}

func finishSetup(prefix string) error {
	home, _ := os.UserHomeDir()
	appDir := os.Getenv("HOMEGREW_APPDIR")
	if appDir == "" {
		appDir = filepath.Join(home, "Applications")
	}

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

func primaryGroup(u *user.User) string {
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return u.Gid
	}
	return g.Name
}
