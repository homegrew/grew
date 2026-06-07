// Package hooks defines lifecycle hook interfaces and phase dispatch for
// formula build, test, and post-install events.
//
// All concrete hook implementations that invoke external commands must use
// pkg/sandbox — never raw os/exec.
package hooks

import (
	"bytes"
	"context"
	"fmt"

	"github.com/homegrew/grew/pkg/sandbox"
)

// Phase identifies a lifecycle event in the formula install pipeline.
type Phase string

const (
	PhasePreBuild    Phase = "pre-build"
	PhasePostBuild   Phase = "post-build"
	PhasePreTest     Phase = "pre-test"
	PhasePostTest    Phase = "post-test"
	PhasePostInstall Phase = "post-install"
)

// Env carries the paths and identity needed by a hook at runtime.
// All fields are absolute paths derived from ctx.Paths — never hardcoded.
type Env struct {
	Prefix  string // grew install prefix (e.g. /opt/homegrew)
	Cellar  string // Cellar root (e.g. /opt/homegrew/Cellar)
	Formula string // formula name
	Version string // formula version
	Tmpdir  string // writable scratch directory for the hook
}

// Hook is a single lifecycle action that can be executed with an Env.
type Hook interface {
	Name() string
	Run(ctx context.Context, env Env) error
}

// HookSet groups hooks by lifecycle phase.
type HookSet struct {
	PreBuild    []Hook
	PostBuild   []Hook
	PreTest     []Hook
	PostTest    []Hook
	PostInstall []Hook
}

// RunPhase executes every hook registered for phase in order. If any hook
// fails, execution stops and an error containing the hook's Name() is
// returned. RunPhase on an empty (or nil) slice is a no-op.
func (hs *HookSet) RunPhase(ctx context.Context, phase Phase, env Env) error {
	for _, h := range hs.sliceFor(phase) {
		if err := h.Run(ctx, env); err != nil {
			return fmt.Errorf("hook %q (%s): %w", h.Name(), phase, err)
		}
	}
	return nil
}

func (hs *HookSet) sliceFor(phase Phase) []Hook {
	if hs == nil {
		return nil
	}
	switch phase {
	case PhasePreBuild:
		return hs.PreBuild
	case PhasePostBuild:
		return hs.PostBuild
	case PhasePreTest:
		return hs.PreTest
	case PhasePostTest:
		return hs.PostTest
	case PhasePostInstall:
		return hs.PostInstall
	default:
		return nil
	}
}

// SandboxedHook runs a command inside a pkg/sandbox post-install sandbox.
// Stdout and stderr are captured and included in any error message.
type SandboxedHook struct {
	name string
	cmd  string
	args []string
}

// NewSandboxedHook creates a hook that runs cmd (with args) inside the
// post-install sandbox. cmd must be an absolute path or a name resolvable
// via PATH; args must not include the command name itself.
func NewSandboxedHook(name, cmd string, args ...string) *SandboxedHook {
	return &SandboxedHook{name: name, cmd: cmd, args: args}
}

// Name returns the hook's identifier.
func (h *SandboxedHook) Name() string { return h.name }

// Run executes the command in a post-install sandbox rooted at env.Cellar
// with env.Tmpdir as the writable scratch space.
func (h *SandboxedHook) Run(_ context.Context, env Env) error {
	cfg := sandbox.PostInstallConfig{
		KegDir: env.Cellar,
		TmpDir: env.Tmpdir,
	}
	var buf bytes.Buffer
	c := sandbox.PostInstallCommand(cfg, h.cmd, h.args...)
	c.Dir = env.Tmpdir
	c.Stdout = &buf
	c.Stderr = &buf
	if err := c.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, buf.String())
	}
	return nil
}

// NoopHook is a test double that records whether it was called.
type NoopHook struct {
	name   string
	called bool
	err    error // if non-nil, Run returns this error
}

// NewNoopHook returns a NoopHook with the given name. If err is non-nil,
// Run will return it.
func NewNoopHook(name string, err error) *NoopHook {
	return &NoopHook{name: name, err: err}
}

// Name returns the hook identifier.
func (h *NoopHook) Name() string { return h.name }

// Run marks the hook as called and returns the configured error (if any).
func (h *NoopHook) Run(_ context.Context, _ Env) error {
	h.called = true
	return h.err
}

// WasCalled reports whether Run has been invoked at least once.
func (h *NoopHook) WasCalled() bool { return h.called }
