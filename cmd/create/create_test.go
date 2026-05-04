package create

import (
	"net/url"
	"testing"
)

func TestInferNameVersion(t *testing.T) {
	tests := []struct {
		url      string
		expectedName string
		expectedVer  string
	}{
		{"https://example.com/foo-1.2.3.tar.gz", "foo", "1.2.3"},
		{"https://example.com/bar_2.0.tgz", "bar", "2.0"},
		{"https://example.com/baz@3.4.zip", "baz", "3.4"},
		{"https://github.com/user/repo/archive/refs/tags/v1.0.0.tar.gz", "repo", "1.0.0"},
		{"https://example.com/simple.tar.gz", "simple", "0.1.0"},
	}

	for _, tt := range tests {
		u, _ := url.Parse(tt.url)
		name, ver := inferNameVersion(u)
		if name != tt.expectedName || ver != tt.expectedVer {
			t.Errorf("for %s: expected %s / %s, got %s / %s", tt.url, tt.expectedName, tt.expectedVer, name, ver)
		}
	}
}

func TestInferHomepage(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://github.com/homegrew/grew/archive/v1.0.tar.gz", "https://github.com/homegrew/grew"},
		{"https://example.com/downloads/tool.zip", "https://example.com"},
	}

	for _, tt := range tests {
		u, _ := url.Parse(tt.url)
		got := inferHomepage(u)
		if got != tt.expected {
			t.Errorf("for %s: expected %s, got %s", tt.url, tt.expected, got)
		}
	}
}
