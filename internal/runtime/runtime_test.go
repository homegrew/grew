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

func TestInit_RequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, cannot test non-root rejection")
	}
	// Without --unsafe, non-root should be rejected even in devmode builds.
	Unsafe = false
	defer func() { Unsafe = false }()

	err := Init()
	if err == nil {
		t.Fatal("Init() should fail for non-root without --unsafe")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("expected root-related error, got: %v", err)
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
