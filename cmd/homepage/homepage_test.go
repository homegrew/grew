package homepage

import (
	"testing"
)

func TestOpenURLDetection(t *testing.T) {
	// This is just to ensure the function exists and compiles.
	// Since it depends on runtime.GOOS, we can't easily test it cross-platform
	// without mocking exec.Command.
	if openURL == nil {
		t.Fatal("openURL function should not be nil")
	}
}
