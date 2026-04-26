package sandbox

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBwrapArgs(t *testing.T) {
	cfg := BuildConfig{
		BuildDir: "/home/user/build",
		KegDir:   "/home/user/.grew/Cellar/foo/1.0",
		DepPaths: []string{"/home/user/.grew/opt/bar"},
	}
	args := bwrapArgs(cfg, "make", "-j4")
	joined := strings.Join(args, " ")

	// Network isolation.
	if !strings.Contains(joined, "--unshare-net") {
		t.Error("bwrap must unshare network namespace")
	}

	// PID isolation.
	if !strings.Contains(joined, "--unshare-pid") {
		t.Error("bwrap must unshare PID namespace")
	}

	// Root filesystem read-only.
	if !strings.Contains(joined, "--ro-bind / /") {
		t.Error("bwrap must bind / read-only")
	}

	// Build dir writable.
	if !strings.Contains(joined, "--bind /home/user/build /home/user/build") {
		t.Error("bwrap must bind build dir read-write")
	}

	// Keg dir writable.
	if !strings.Contains(joined, "--bind /home/user/.grew/Cellar/foo/1.0 /home/user/.grew/Cellar/foo/1.0") {
		t.Error("bwrap must bind keg dir read-write")
	}

	// Fresh /tmp.
	if !strings.Contains(joined, "--tmpfs /tmp") {
		t.Error("bwrap must provide a tmpfs /tmp")
	}

	// /proc inside new PID namespace.
	if !strings.Contains(joined, "--proc /proc") {
		t.Error("bwrap must mount /proc")
	}

	// Minimal /dev.
	if !strings.Contains(joined, "--dev /dev") {
		t.Error("bwrap must mount /dev")
	}

	// Writable bind mounts must come AFTER --tmpfs /tmp so that build
	// dirs under /tmp are not clobbered by the tmpfs overlay.
	tmpfsIdx := strings.Index(joined, "--tmpfs /tmp")
	buildBindIdx := strings.Index(joined, "--bind /home/user/build")
	kegBindIdx := strings.Index(joined, "--bind /home/user/.grew/Cellar")
	if buildBindIdx < tmpfsIdx {
		t.Error("--bind for build dir must come after --tmpfs /tmp")
	}
	if kegBindIdx < tmpfsIdx {
		t.Error("--bind for keg dir must come after --tmpfs /tmp")
	}

	// Command must appear at the end.
	if !strings.HasSuffix(joined, "make -j4") {
		t.Errorf("command must be at end of args, got: ...%s", joined[len(joined)-30:])
	}
}

func TestUnshareArgs(t *testing.T) {
	cfg := BuildConfig{
		BuildDir: "/home/user/build",
		KegDir:   "/home/user/.grew/Cellar/foo/1.0",
	}
	args := unshareArgs(cfg, "./configure", "--prefix=/home/user/.homegrew/Cellar/foo/1.0")

	// Must use network, mount, and PID namespace flags.
	flagSet := map[string]bool{}
	for _, a := range args {
		flagSet[a] = true
	}
	for _, flag := range []string{"--net", "--mount", "--pid", "--fork", "--mount-proc"} {
		if !flagSet[flag] {
			t.Errorf("unshare must use %s flag", flag)
		}
	}

	// Must invoke /bin/sh -c <script> -- <args...>.
	// Expected end of args: ... /bin/sh -c <script> -- BuildDir KegDir name args...
	foundSh := false
	var script string
	for i, a := range args {
		if a == "/bin/sh" && i+2 < len(args) && args[i+1] == "-c" {
			foundSh = true
			script = args[i+2]
			break
		}
	}
	if !foundSh {
		t.Fatal("unshare must run the setup script via /bin/sh -c")
	}

	// Script must make mounts private.
	if !strings.Contains(script, "mount --make-rprivate /") {
		t.Error("script must make mount propagation private")
	}

	// Script must remount root read-only.
	if !strings.Contains(script, "mount -o remount,ro,bind /") {
		t.Error("script must remount / read-only")
	}

	// Script must bind-mount build dir writable using positional parameters.
	if !strings.Contains(script, "mount --bind \"$1\" \"$1\"") {
		t.Error("script must bind-mount build dir")
	}
	if !strings.Contains(script, "mount -o remount,rw,bind \"$1\"") {
		t.Error("script must remount build dir read-write")
	}

	// Script must bind-mount keg dir writable using positional parameters.
	if !strings.Contains(script, "mount --bind \"$2\" \"$2\"") {
		t.Error("script must bind-mount keg dir")
	}
	if !strings.Contains(script, "mount -o remount,rw,bind \"$2\"") {
		t.Error("script must remount keg dir read-write")
	}

	// Script must mount tmpfs on /tmp.
	if !strings.Contains(script, "mount -t tmpfs tmpfs /tmp") {
		t.Error("script must mount tmpfs on /tmp")
	}

	// Script must exec the command using $@.
	if !strings.Contains(script, "exec \"$@\"") {
		t.Error("script must exec the build command")
	}

	// Verify the positional parameters passed to the script.
	// args should contain: ..., "--", "/bin/sh", "-c", script, "--", BuildDir, KegDir, name, args...
	foundDashDash := false
	for i := len(args) - 1; i >= 0; i-- {
		if args[i] == "--" {
			// We expect the one after /bin/sh -c script
			if i > 0 && args[i-1] == script {
				foundDashDash = true
				params := args[i+1:]
				if len(params) < 4 {
					t.Fatalf("expected at least 4 positional parameters, got %d", len(params))
				}
				if params[0] != cfg.BuildDir {
					t.Errorf("expected build dir %q, got %q", cfg.BuildDir, params[0])
				}
				if params[1] != cfg.KegDir {
					t.Errorf("expected keg dir %q, got %q", cfg.KegDir, params[1])
				}
				if params[2] != "./configure" {
					t.Errorf("expected command %q, got %q", "./configure", params[2])
				}
				if params[3] != "--prefix=/home/user/.grew/Cellar/foo/1.0" {
					t.Errorf("expected arg %q, got %q", "--prefix=/home/user/.grew/Cellar/foo/1.0", params[3])
				}
				break
			}
		}
	}
	if !foundDashDash {
		t.Error("unshare must pass positional parameters to the script after --")
	}
}

func TestSandboxedEchoRunsLinux(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		if _, err := exec.LookPath("unshare"); err != nil {
			t.Skip("neither bwrap nor unshare available")
		}
	}

	dir := t.TempDir()
	cfg := BuildConfig{BuildDir: dir, KegDir: dir}

	cmd := Command(cfg, "echo", "sandboxed")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("sandboxed echo failed: %v", err)
	}
	if !strings.Contains(string(out), "sandboxed") {
		t.Errorf("expected 'sandboxed' in output, got %q", string(out))
	}
}
