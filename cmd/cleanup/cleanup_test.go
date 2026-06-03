package cleanup

import (
	"testing"

	"github.com/homegrew/grew/pkg/cellar"
)

func TestBelongsToTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		targets  []string
		filename string
		want     bool
	}{
		{[]string{"jq"}, "jq-1.6.tar.gz", true},
		{[]string{"jq"}, "nmap-7.92.tar.gz", false},
		{[]string{"jq", "nmap"}, "nmap-7.92.tar.gz", true},
		{[]string{}, "any-1.0.tar.gz", false},
	}

	for _, tt := range tests {
		if got := cellar.BelongsToTargets(tt.targets, tt.filename); got != tt.want {
			t.Errorf("belongsToTargets(%v, %q) = %v, want %v", tt.targets, tt.filename, got, tt.want)
		}
	}
}

func TestIsLatestInstalled(t *testing.T) {
	t.Parallel()
	installed := []cellar.InstalledPackage{
		{Name: "jq", Version: "1.6"},
		{Name: "nmap", Version: "7.92"},
	}

	tests := []struct {
		filename string
		want     bool
	}{
		{"jq-1.6.tar.gz", true},
		{"jq-1.5.tar.gz", false},
		{"nmap-7.92-src.tar.gz", true},
		{"other-1.0.tar.gz", false},
	}

	for _, tt := range tests {
		if got := cellar.IsLatestInstalled(installed, tt.filename); got != tt.want {
			t.Errorf("isLatestInstalled(%q) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}
