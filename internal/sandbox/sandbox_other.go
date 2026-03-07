//go:build !darwin && !linux

package sandbox

import "os/exec"

func platformPostInstallCommand(cfg PostInstallConfig, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = postInstallEnv(cfg)
	return cmd
}

func platformCommand(cfg BuildConfig, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = cleanEnv(cfg)
	return cmd
}
