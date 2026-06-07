// Package sudo provides a single helper for running a command with elevated
// privileges on macOS without hard-coding a terminal password prompt.
//
// The approach avoids spawning an interactive shell or requiring the caller to
// configure sudoers. Instead it writes a short AppleScript helper to a
// temporary file, sets SUDO_ASKPASS to point at it, and invokes
// /usr/bin/sudo -A. The system dialog appears in the foreground GUI session;
// the helper is removed after the call regardless of outcome.
//
// This package is used only by `grew setup`, which is the one operation in
// grew's lifecycle that requires root — creating and owning the system prefix
// (/opt/homegrew or /usr/local/homegrew). All subsequent operations run as
// the unprivileged user.
package sudo
