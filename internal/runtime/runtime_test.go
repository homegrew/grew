package runtime

import (
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
