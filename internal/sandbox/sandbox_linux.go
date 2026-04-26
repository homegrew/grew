package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

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
	bwrapExe, _ = exec.LookPath("bwrap")
	unshareExe, _ = exec.LookPath("unshare")
}

// BwrapAvailable probes whether bwrap can actually create the namespaces
// we need. On many systems (containers, restrictive kernels) unprivileged
// namespace creation is blocked even though bwrap is installed.
func BwrapAvailable() bool {
	if bwrapExe == "" {
		return false
	}
	a := bwrapBaseArgs()
	a = append(a, "--", "true")
	cmd := exec.Command(bwrapExe, a...)
	return cmd.Run() == nil
}

// UnshareAvailable probes whether unshare(1) can create a network namespace.
func UnshareAvailable() bool {
	if unshareExe == "" {
		return false
	}
	cmd := exec.Command(unshareExe, "--net", "--", "true")
	return cmd.Run() == nil
}

func bwrapBaseArgs() []string {
	a := []string{
		"--unshare-net",
		"--unshare-pid",
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
		"--proc", "/proc",
		"--dev", "/dev",
	}

	if target, err := os.Readlink("/lib64"); err == nil {
		a = append(a, "--symlink", target, "/lib64")
	}

	if fi, err := os.Stat("/var/tmp"); err == nil && fi.IsDir() {
		a = append(a, "--tmpfs", "/var/tmp")
	}
	return a
}

func writeBaseUnshareScript(script *strings.Builder) {
	script.WriteString("set -e\n")
	script.WriteString("mount --bind / /\n")
	script.WriteString("mount --make-rprivate /\n")
	script.WriteString("mount -o remount,ro,bind /\n")
}

func writeUnshareMountScript(script *strings.Builder, numWritable int) {
	writeBaseUnshareScript(script)
	for i := 1; i <= numWritable; i++ {
		fmt.Fprintf(script, "mount --bind \"$%d\" \"$%d\"\n", i, i)
		fmt.Fprintf(script, "mount -o remount,rw,bind \"$%d\"\n", i)
	}
	script.WriteString("mount -t tmpfs tmpfs /tmp\n")
	if numWritable > 0 {
		fmt.Fprintf(script, "shift %d\n", numWritable)
	}
	script.WriteString("exec \"$@\"\n")
}

func platformIsSandboxed() bool {
	return BwrapAvailable() || UnshareAvailable()
}

func platformExtractCommand(cfg ExtractConfig, name string, args ...string) *exec.Cmd {
	// Extraction sandbox: network denied, only StageDir writable.
	// Uses same tiered approach as post-install: bwrap > unshare > direct.
	if BwrapAvailable() {
		a := bwrapBaseArgs()
		a = append(a, "--bind", cfg.StageDir, cfg.StageDir)
		a = append(a, "--", name)
		a = append(a, args...)
		cmd := exec.Command(bwrapExe, a...)
		cmd.Env = extractEnv(cfg)
		return cmd
	}
	if UnshareAvailable() {
		return unshareExtractCommand(cfg, name, args...)
	}
	// Fallback: run directly (still benefits from Go-level protections).
	cmd := exec.Command(name, args...)
	cmd.Env = extractEnv(cfg)
	return cmd
}

func unshareExtractCommand(cfg ExtractConfig, name string, args ...string) *exec.Cmd {
	scriptFile, err := writeUnshareScript(cfg.StageDir, func(script *strings.Builder) {
		writeUnshareMountScript(script, 1)
	})
	if err != nil {
		cmd := exec.Command(name, args...)
		cmd.Env = extractEnv(cfg)
		return cmd
	}

	// unshare [options] [--] /bin/sh scriptFile [args...]
	a := []string{
		"--net", "--mount", "--pid", "--fork", "--mount-proc",
		"--",
		"/bin/sh", scriptFile, cfg.StageDir, name,
	}
	a = append(a, args...)

	cmd := exec.Command(unshareExe, a...)
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
	a := bwrapBaseArgs()

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
	scriptFile, err := writeUnshareScript(cfg.TmpDir, func(script *strings.Builder) {
		writeUnshareMountScript(script, 1)
	})
	if err != nil {
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
	a := bwrapBaseArgs()

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
		writeUnshareMountScript(script, 2)
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
	writeBaseUnshareScript(&script)
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
