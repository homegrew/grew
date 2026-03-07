package sandbox

import (
	"fmt"
	"os/exec"
	"strings"
)

func platformCommand(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	profile := seatbeltProfile(cfg)

	sandboxArgs := []string{"-p", profile, name}
	sandboxArgs = append(sandboxArgs, args...)
	cmd := exec.Command("sandbox-exec", sandboxArgs...)
	cmd.Env = cleanEnv(cfg)
	return cmd
}

// seatbeltProfile generates a macOS Seatbelt sandbox profile that:
//   - Denies all network access
//   - Allows file reads everywhere (needed for toolchains, dyld cache, etc.)
//   - Restricts file writes to: build dir, keg dir, /dev (stdout/stderr),
//     and /private/var/folders (compiler temp cache)
func seatbeltProfile(cfg BuildConfig) string {
	var b strings.Builder

	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")

	// Process operations needed by compilers and build tools.
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow signal)\n")
	b.WriteString("(allow sysctl*)\n")
	b.WriteString("(allow mach*)\n")
	b.WriteString("(allow ipc*)\n")

	// Deny all network access — the source is already downloaded.
	b.WriteString("(deny network*)\n")

	// Allow all file reads — builds need system headers, dyld cache,
	// Xcode toolchains, SDK paths, etc.
	b.WriteString("(allow file-read*)\n")

	// Write access: only to specific directories.
	fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", cfg.BuildDir)
	fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", cfg.KegDir)
	b.WriteString("(allow file-write* (subpath \"/dev\"))\n")
	b.WriteString("(allow file-write* (subpath \"/private/var/folders\"))\n")

	return b.String()
}
