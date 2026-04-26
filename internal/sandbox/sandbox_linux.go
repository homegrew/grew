package sandbox

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"al.essio.dev/pkg/shellescape"
)

// shq sanitizes and shell-quotes a string for safe interpolation into shell scripts.
// It strips non-printable characters (including null bytes) then applies POSIX
// single-quote escaping via shellescape.Quote.
func shq(s string) string {
	return shellescape.Quote(shellescape.StripUnsafe(s))
}

// writeUnshareScript writes a shell setup script to a temp file and returns
// its path. Using a file instead of sh -c prevents shell injection via
// interpolated paths. The caller passes a callback that writes the script
// content into the provided builder.
func writeUnshareScript(tmpDir string, build func(*strings.Builder)) (string, error) {
	var script strings.Builder
	build(&script)

	f, err := os.CreateTemp(tmpDir, "grew-sandbox-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(script.String()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

var bwrapExe, unshareExe string

func init() {
	var errBwrap, errUnshare error

	bwrapExe, errBwrap = exec.LookPath("bwrap")
	unshareExe, errUnshare = exec.LookPath("unshare")

	if errBwrap != nil {
		slog.Warn("bwrap not found in $PATH")
	}

	if errUnshare != nil {
		slog.Warn("unshare not found in $PATH")
	}
}

// bwrapAvailable probes whether bwrap can actually create the namespaces
// we need. On many systems (containers, restrictive kernels) unprivileged
// namespace creation is blocked even though bwrap is installed.
func BwrapAvailable() bool {
	if bwrapExe == "" {
		return false
	}
	cmd := exec.Command(bwrapExe,
		"--ro-bind", "/", "/",
		"--unshare-net",
		"--unshare-pid",
		"--proc", "/proc",
		"--dev", "/dev",
		"--",
		"true",
	)
	return cmd.Run() == nil
}

// unshareAvailable probes whether unshare(1) can create a network namespace.
func UnshareAvailable() bool {
	if unshareExe == "" {
		return false
	}
	cmd := exec.Command(unshareExe, "--net", "--", "true")
	return cmd.Run() == nil
}

func platformIsSandboxed() bool {
	return BwrapAvailable() || UnshareAvailable()
}

func platformExtractCommand(cfg ExtractConfig, name string, args ...string) *exec.Cmd {
	// Extraction sandbox: network denied, only StageDir writable.
	// Uses same tiered approach as post-install: bwrap > unshare > direct.
	if BwrapAvailable() {
		a := []string{
			"--unshare-net",
			"--unshare-pid",
			"--ro-bind", "/", "/",
			"--tmpfs", "/tmp",
			"--proc", "/proc",
			"--dev", "/dev",
		}

		if fi, err := os.Stat("/var/tmp"); err == nil && fi.IsDir() {
			a = append(a, "--tmpfs", "/var/tmp")
		}

		a = append(a, "--bind", cfg.StageDir, cfg.StageDir)
		a = append(a, "--", name)
		a = append(a, args...)
		cmd := exec.Command(bwrapExe, a...)
		cmd.Env = extractEnv(cfg)
		return cmd
	}
	// Fallback: run directly (still benefits from Go-level protections).
	cmd := exec.Command(name, args...)
	cmd.Env = extractEnv(cfg)
	return cmd
}

func platformPostInstallCommand(cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	if BwrapAvailable() {
		return bwrapPostInstallCommand(cfg, name, args...)
	}
	if UnshareAvailable() {
		return unsharePostInstallCommand(cfg, name, args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = postInstallEnv(cfg)
	return cmd
}

func bwrapPostInstallCommand(cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	a := []string{
		"--unshare-net",
		"--unshare-pid",
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
		"--proc", "/proc",
		"--dev", "/dev",
	}

	if fi, err := os.Stat("/var/tmp"); err == nil && fi.IsDir() {
		a = append(a, "--tmpfs", "/var/tmp")
	}

	// Keg is already read-only via the ro-bind of /.
	// Only the tmp dir is writable.
	a = append(a, "--bind", cfg.TmpDir, cfg.TmpDir)

	a = append(a, "--", name)
	a = append(a, args...)
	cmd := exec.Command(bwrapExe, a...)
	cmd.Env = postInstallEnv(cfg)
	return cmd
}

func unsharePostInstallCommand(cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	// Write the setup script to a temp file. We pass dynamic values (paths,
	// command, args) as positional parameters to the script to avoid shell
	// injection risks from direct interpolation.
	scriptFile, err := writeUnshareScript(cfg.TmpDir, func(script *strings.Builder) {
		script.WriteString("set -e\n")
		script.WriteString("mount --bind / /\n")
		script.WriteString("mount --make-rprivate /\n")
		script.WriteString("mount -o remount,ro,bind /\n")
		// $1 is cfg.TmpDir. Only tmp dir is writable.
		script.WriteString("mount --bind \"$1\" \"$1\"\n")
		script.WriteString("mount -o remount,rw,bind \"$1\"\n")
		script.WriteString("mount -t tmpfs tmpfs /tmp\n")
		if fi, err := os.Stat("/var/tmp"); err == nil && fi.IsDir() {
			script.WriteString("mount -t tmpfs tmpfs /var/tmp\n")
		}
		script.WriteString("shift\n") // Remove cfg.TmpDir from positional params.
		script.WriteString("exec \"$@\"\n")
	})
	if err != nil {
		// Fall back to direct execution without unshare sandboxing.
		cmd := exec.Command(name, args...)
		cmd.Env = postInstallEnv(cfg)
		return cmd
	}

	// unshare [options] [--] /bin/sh scriptFile [args...]
	a := []string{
		"--net", "--mount", "--pid", "--fork", "--mount-proc",
		"--",
		"/bin/sh", scriptFile, cfg.TmpDir, name,
	}
	a = append(a, args...)

	cmd := exec.Command(unshareExe, a...)
	cmd.Env = postInstallEnv(cfg)
	return cmd
}

func platformCommand(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	if BwrapAvailable() {
		return bwrapCommand(cfg, name, args...)
	}
	if UnshareAvailable() {
		return unshareCommand(cfg, name, args...)
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
func bwrapCommand(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	a := bwrapArgs(cfg, name, args...)
	cmd := exec.Command(bwrapExe, a...)
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
	a = append(a, "--", name)
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
func unshareCommand(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	// Write the setup script to a temp file. We pass dynamic values (paths,
	// command, args) as positional parameters to the script to avoid shell
	// injection risks from direct interpolation.
	tmpDir := os.TempDir()
	scriptFile, err := writeUnshareScript(tmpDir, func(script *strings.Builder) {
		script.WriteString("set -e\n")
		// Prevent mount propagation to the host.
		script.WriteString("mount --make-rprivate /\n")
		// Remount root read-only.
		script.WriteString("mount -o remount,ro,bind /\n")
		// $1 is cfg.BuildDir, $2 is cfg.KegDir.
		// Writable bind-mount for the build dir.
		script.WriteString("mount --bind \"$1\" \"$1\"\n")
		script.WriteString("mount -o remount,rw,bind \"$1\"\n")
		// Writable bind-mount for the keg dir.
		script.WriteString("mount --bind \"$2\" \"$2\"\n")
		script.WriteString("mount -o remount,rw,bind \"$2\"\n")
		// Fresh tmpfs for /tmp.
		script.WriteString("mount -t tmpfs tmpfs /tmp\n")
		script.WriteString("shift 2\n") // Remove BuildDir and KegDir from positional params.
		script.WriteString("exec \"$@\"\n")
	})
	if err != nil {
		// Fall back to direct execution without unshare sandboxing.
		cmd := exec.Command(name, args...)
		cmd.Env = cleanEnv(cfg)
		return cmd
	}

	// unshare [options] [--] /bin/sh scriptFile [args...]
	a := []string{
		"--net", "--mount", "--pid", "--fork", "--mount-proc",
		"--",
		"/bin/sh", scriptFile, cfg.BuildDir, cfg.KegDir, name,
	}
	a = append(a, args...)

	cmd := exec.Command(unshareExe, a...)
	cmd.Env = cleanEnv(cfg)
	return cmd
}

// unshareArgs builds the argument list for unshare(1). Exported-via-name so
// tests can validate the generated arguments without running on Linux.
func unshareArgs(cfg BuildConfig, name string, args ...string) []string {
	// Build the shell script that sets up the mount namespace.
	var script strings.Builder
	script.WriteString("set -e; ")
	script.WriteString("mount --make-rprivate /; ")
	script.WriteString("mount -o remount,ro,bind /; ")
	// $1 = BuildDir, $2 = KegDir.
	script.WriteString("mount --bind \"$1\" \"$1\"; ")
	script.WriteString("mount -o remount,rw,bind \"$1\"; ")
	script.WriteString("mount --bind \"$2\" \"$2\"; ")
	script.WriteString("mount -o remount,rw,bind \"$2\"; ")
	script.WriteString("mount -t tmpfs tmpfs /tmp; ")
	script.WriteString("shift 2; ")
	script.WriteString("exec \"$@\"")

	a := []string{
		"--net",
		"--mount",
		"--pid",
		"--fork",
		"--mount-proc",
		"--",
		"/bin/sh", "-c", script.String(), "--", cfg.BuildDir, cfg.KegDir, name,
	}
	return append(a, args...)
}
