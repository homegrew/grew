package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var testEnv = Env{
	Prefix:  "/opt/homegrew",
	Cellar:  "/opt/homegrew/Cellar",
	Formula: "testpkg",
	Version: "1.0",
	Tmpdir:  "/tmp/grew-test",
}

func TestRunPhase_CallsHook(t *testing.T) {
	h := NewNoopHook("my-hook", nil)
	hs := &HookSet{PostBuild: []Hook{h}}

	if err := hs.RunPhase(context.Background(), PhasePostBuild, testEnv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.WasCalled() {
		t.Error("hook was not called")
	}
}

func TestRunPhase_ErrorContainsHookName(t *testing.T) {
	want := errors.New("something went wrong")
	h := NewNoopHook("failing-hook", want)
	hs := &HookSet{PostInstall: []Hook{h}}

	err := hs.RunPhase(context.Background(), PhasePostInstall, testEnv)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failing-hook") {
		t.Errorf("error should contain hook name, got: %v", err)
	}
	if !errors.Is(err, want) {
		t.Errorf("error should wrap original: %v", err)
	}
}

func TestRunPhase_EmptySlice_NoOp(t *testing.T) {
	hs := &HookSet{}
	for _, phase := range []Phase{PhasePreBuild, PhasePostBuild, PhasePreTest, PhasePostTest, PhasePostInstall} {
		if err := hs.RunPhase(context.Background(), phase, testEnv); err != nil {
			t.Errorf("phase %s: unexpected error on empty slice: %v", phase, err)
		}
	}
}

func TestRunPhase_NilHookSet_NoOp(t *testing.T) {
	var hs *HookSet
	if err := hs.RunPhase(context.Background(), PhasePostBuild, testEnv); err != nil {
		t.Errorf("nil HookSet: unexpected error: %v", err)
	}
}

func TestRunPhase_StopsOnFirstError(t *testing.T) {
	sentinel := errors.New("stop here")
	first := NewNoopHook("first", sentinel)
	second := NewNoopHook("second", nil)
	hs := &HookSet{PostBuild: []Hook{first, second}}

	if err := hs.RunPhase(context.Background(), PhasePostBuild, testEnv); err == nil {
		t.Fatal("expected error")
	}
	if second.WasCalled() {
		t.Error("second hook should not have been called after first failed")
	}
}

func TestRunPhase_AllPhasesDispatched(t *testing.T) {
	hooks := map[Phase]*NoopHook{
		PhasePreBuild:    NewNoopHook("pre-build", nil),
		PhasePostBuild:   NewNoopHook("post-build", nil),
		PhasePreTest:     NewNoopHook("pre-test", nil),
		PhasePostTest:    NewNoopHook("post-test", nil),
		PhasePostInstall: NewNoopHook("post-install", nil),
	}
	hs := &HookSet{
		PreBuild:    []Hook{hooks[PhasePreBuild]},
		PostBuild:   []Hook{hooks[PhasePostBuild]},
		PreTest:     []Hook{hooks[PhasePreTest]},
		PostTest:    []Hook{hooks[PhasePostTest]},
		PostInstall: []Hook{hooks[PhasePostInstall]},
	}
	ctx := context.Background()
	for phase, h := range hooks {
		if err := hs.RunPhase(ctx, phase, testEnv); err != nil {
			t.Errorf("phase %s: %v", phase, err)
		}
		if !h.WasCalled() {
			t.Errorf("phase %s: hook not called", phase)
		}
	}
}

func TestNoopHook_Name(t *testing.T) {
	h := NewNoopHook("my-name", nil)
	if h.Name() != "my-name" {
		t.Errorf("Name() = %q, want my-name", h.Name())
	}
}
