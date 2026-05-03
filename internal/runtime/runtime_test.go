package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestSystemPrefix(t *testing.T) {
	p := SystemPrefix()
	if !strings.HasPrefix(p, "/opt/") && !strings.HasPrefix(p, "/usr/local/") {
		t.Errorf("SystemPrefix() = %q, want /opt/homegrew or /usr/local/homegrew", p)
	}
}

func TestUserPrefix(t *testing.T) {
	p, err := UserPrefix()
	if err != nil {
		t.Fatalf("UserPrefix() error: %v", err)
	}
	if !strings.HasSuffix(p, ".homegrew") {
		t.Errorf("UserPrefix() = %q, should end with .homegrew", p)
	}
}

func TestInit_NonRootSystemPrefix(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, cannot test non-root behavior")
	}
	// Reset runtime state for test.
	r = nil
	Unsafe = false
	defer func() { Unsafe = false; r = nil }()

	err := Init()
	if err != nil {
		t.Fatalf("Init() should succeed for non-root: %v", err)
	}

	env := Env()
	if env.RunAsRoot() {
		t.Error("expected RunAsRoot() = false")
	}
	if env.DefaultPrefix() != SystemPrefix() {
		t.Errorf("expected system prefix %q, got %q", SystemPrefix(), env.DefaultPrefix())
	}
}

func TestInit_DevModeUnsafe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, cannot test non-root path")
	}
	if !DevMode {
		t.Skip("not a devmode build")
	}

	Unsafe = true
	defer func() { Unsafe = false; r = nil }()

	err := Init()
	if err != nil {
		t.Fatalf("Init() with devmode+unsafe should succeed: %v", err)
	}
	env := Env()
	if env.RunAsRoot() {
		t.Error("expected RunAsRoot() = false")
	}
	if !strings.HasSuffix(env.DefaultPrefix(), ".homegrew") {
		t.Errorf("expected user prefix, got %q", env.DefaultPrefix())
	}
}
