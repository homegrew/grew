package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func platformCommand(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	if p, err := exec.LookPath("bwrap"); err == nil {
		return bwrapCommand(p, cfg, name, args...)
	}
	if p, err := exec.LookPath("unshare"); err == nil {
		return unshareCommand(p, cfg, name, args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = cleanEnv(cfg)
	return cmd
}

// bwrapCommand uses bubblewrap for full namespace-based isolation:
//   - Separate network namespace (no connectivity)
//   - Root filesystem bind-mounted read-only
//   - Writable overlays for build dir, keg dir, and /tmp
//   - Fresh /proc and /dev
func bwrapCommand(bwrapPath string, cfg BuildConfig, name string, args ...string) *exec.Cmd {
	a := bwrapArgs(cfg, name, args...)
	cmd := exec.Command(bwrapPath, a...)
	cmd.Env = cleanEnv(cfg)
	return cmd
}

// bwrapArgs builds the argument list for bubblewrap. Exported-via-name so
// tests can validate the generated arguments without running on Linux.
func bwrapArgs(cfg BuildConfig, name string, args ...string) []string {
	a := []string{
		// New network namespace — completely isolated, no interfaces.
		"--unshare-net",
		// New PID namespace — build processes can't signal the host.
		"--unshare-pid",
		// Bind the entire root filesystem read-only.
		"--ro-bind", "/", "/",
		// Writable bind for build directory.
		"--bind", cfg.BuildDir, cfg.BuildDir,
		// Writable bind for keg (install prefix).
		"--bind", cfg.KegDir, cfg.KegDir,
		// Fresh tmpfs for compiler temporaries.
		"--tmpfs", "/tmp",
		// Mount /proc inside the new PID namespace.
		"--proc", "/proc",
		// Minimal /dev with standard devices only (null, zero, urandom, etc.).
		"--dev", "/dev",
	}

	// Some distros have /lib64 as a real directory rather than a symlink.
	// If it exists as a symlink (e.g. /lib64 -> /usr/lib64), the ro-bind
	// of / already covers it. If it doesn't exist, --symlink is harmless.
	if target, err := os.Readlink("/lib64"); err == nil {
		a = append(a, "--symlink", target, "/lib64")
	}

	// /var/tmp is often used by build systems and is separate from /tmp.
	if fi, err := os.Stat("/var/tmp"); err == nil && fi.IsDir() {
		a = append(a, "--tmpfs", "/var/tmp")
	}

	// The command to run inside the sandbox.
	a = append(a, name)
	a = append(a, args...)
	return a
}

// unshareCommand uses Linux unshare(1) to create new namespaces and then
// remounts the root filesystem read-only, bind-mounting the build and keg
// directories as writable. This is the fallback when bwrap is not installed.
//
// Namespace isolation provided:
//   - Network namespace  (--net): no network interfaces
//   - Mount namespace    (--mount): private mount table
//   - PID namespace      (--pid --fork --mount-proc): isolated process tree
func unshareCommand(unsharePath string, cfg BuildConfig, name string, args ...string) *exec.Cmd {
	a := unshareArgs(cfg, name, args...)
	cmd := exec.Command(unsharePath, a...)
	cmd.Env = cleanEnv(cfg)
	return cmd
}

// unshareArgs builds the argument list for unshare(1). The approach:
//  1. Create network + mount + PID namespaces
//  2. Run a shell that remounts / read-only, then bind-mounts the
//     build and keg dirs as writable before exec-ing the real command.
func unshareArgs(cfg BuildConfig, name string, args ...string) []string {
	// Build the shell script that sets up the mount namespace.
	// After unshare creates the namespaces, this script:
	//   a) Makes all existing mounts private (no propagation to host)
	//   b) Remounts / as read-only recursively
	//   c) Bind-mounts build/keg dirs as writable
	//   d) Execs the actual build command
	var script strings.Builder
	script.WriteString("set -e; ")
	// Prevent mount propagation to the host.
	script.WriteString("mount --make-rprivate /; ")
	// Remount root read-only.
	script.WriteString("mount -o remount,ro,bind /; ")
	// Writable bind-mount for the build dir.
	fmt.Fprintf(&script, "mount --bind %q %q; ", cfg.BuildDir, cfg.BuildDir)
	fmt.Fprintf(&script, "mount -o remount,rw,bind %q; ", cfg.BuildDir)
	// Writable bind-mount for the keg dir.
	fmt.Fprintf(&script, "mount --bind %q %q; ", cfg.KegDir, cfg.KegDir)
	fmt.Fprintf(&script, "mount -o remount,rw,bind %q; ", cfg.KegDir)
	// Fresh tmpfs for /tmp.
	script.WriteString("mount -t tmpfs tmpfs /tmp; ")
	// Exec the build command.
	fmt.Fprintf(&script, "exec %q", name)
	for _, a := range args {
		fmt.Fprintf(&script, " %q", a)
	}

	return []string{
		"--net",
		"--mount",
		"--pid",
		"--fork",
		"--mount-proc",
		"/bin/sh", "-c", script.String(),
	}
}
