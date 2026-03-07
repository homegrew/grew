package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func platformPostInstallCommand(cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	if p, err := exec.LookPath("bwrap"); err == nil && bwrapAvailable(p) {
		return bwrapPostInstallCommand(p, cfg, name, args...)
	}
	if p, err := exec.LookPath("unshare"); err == nil && unshareAvailable(p) {
		return unsharePostInstallCommand(p, cfg, name, args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = postInstallEnv(cfg)
	return cmd
}

func bwrapPostInstallCommand(bwrapPath string, cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	a := []string{
		"--unshare-net",
		"--unshare-pid",
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
		"--proc", "/proc",
		"--dev", "/dev",
		// Keg is already read-only via the ro-bind of /.
		// Only the tmp dir is writable.
		"--bind", cfg.TmpDir, cfg.TmpDir,
	}
	a = append(a, name)
	a = append(a, args...)
	cmd := exec.Command(bwrapPath, a...)
	cmd.Env = postInstallEnv(cfg)
	return cmd
}

func unsharePostInstallCommand(unsharePath string, cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	var script strings.Builder
	script.WriteString("set -e; ")
	script.WriteString("mount --make-rprivate /; ")
	script.WriteString("mount -o remount,ro,bind /; ")
	// Only tmp dir is writable.
	fmt.Fprintf(&script, "mount --bind %q %q; ", cfg.TmpDir, cfg.TmpDir)
	fmt.Fprintf(&script, "mount -o remount,rw,bind %q; ", cfg.TmpDir)
	script.WriteString("mount -t tmpfs tmpfs /tmp; ")
	fmt.Fprintf(&script, "exec %q", name)
	for _, a := range args {
		fmt.Fprintf(&script, " %q", a)
	}
	cmd := exec.Command(unsharePath,
		"--net", "--mount", "--pid", "--fork", "--mount-proc",
		"/bin/sh", "-c", script.String(),
	)
	cmd.Env = postInstallEnv(cfg)
	return cmd
}

func platformCommand(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	if p, err := exec.LookPath("bwrap"); err == nil && bwrapAvailable(p) {
		return bwrapCommand(p, cfg, name, args...)
	}
	if p, err := exec.LookPath("unshare"); err == nil && unshareAvailable(p) {
		return unshareCommand(p, cfg, name, args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = cleanEnv(cfg)
	return cmd
}

// bwrapAvailable probes whether bwrap can actually create the namespaces
// we need. On many systems (containers, restrictive kernels) unprivileged
// namespace creation is blocked even though bwrap is installed.
func bwrapAvailable(bwrapPath string) bool {
	cmd := exec.Command(bwrapPath,
		"--ro-bind", "/", "/",
		"--unshare-net",
		"--unshare-pid",
		"--proc", "/proc",
		"--dev", "/dev",
		"true",
	)
	return cmd.Run() == nil
}

// unshareAvailable probes whether unshare(1) can create a network namespace.
func unshareAvailable(unsharePath string) bool {
	cmd := exec.Command(unsharePath, "--net", "true")
	return cmd.Run() == nil
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
	// bwrap processes filesystem args in order — later entries overlay
	// earlier ones. The sequence matters:
	//   1. ro-bind /         → read-only root
	//   2. tmpfs /tmp        → fresh, empty /tmp
	//   3. tmpfs /var/tmp    → fresh, empty /var/tmp
	//   4. proc, dev         → isolated /proc and /dev
	//   5. bind buildDir     → writable build dir (may be under /tmp)
	//   6. bind kegDir       → writable keg dir
	// Steps 5-6 MUST come after the tmpfs mounts so that writable bind
	// mounts for paths under /tmp are not clobbered.
	a := []string{
		// New network namespace — completely isolated, no interfaces.
		"--unshare-net",
		// New PID namespace — build processes can't signal the host.
		"--unshare-pid",
		// Bind the entire root filesystem read-only.
		"--ro-bind", "/", "/",
		// Fresh tmpfs for compiler temporaries — before writable binds.
		"--tmpfs", "/tmp",
		// Mount /proc inside the new PID namespace.
		"--proc", "/proc",
		// Minimal /dev with standard devices only (null, zero, urandom, etc.).
		"--dev", "/dev",
	}

	// Some distros have /lib64 as a real directory rather than a symlink.
	if target, err := os.Readlink("/lib64"); err == nil {
		a = append(a, "--symlink", target, "/lib64")
	}

	// /var/tmp is often used by build systems and is separate from /tmp.
	if fi, err := os.Stat("/var/tmp"); err == nil && fi.IsDir() {
		a = append(a, "--tmpfs", "/var/tmp")
	}

	// Writable bind mounts AFTER tmpfs overlays so they are not clobbered.
	a = append(a, "--bind", cfg.BuildDir, cfg.BuildDir)
	a = append(a, "--bind", cfg.KegDir, cfg.KegDir)

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
