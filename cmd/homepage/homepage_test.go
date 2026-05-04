package homepage

import (
	"testing"
)

func TestOpenURLDetection(t *testing.T) {
	// This is just to ensure the package compiles and the command is correctly initialized.
	if Command == nil {
		t.Fatal("homepage command should not be nil")
	}
}
