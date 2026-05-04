package safepath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePathComponent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "foo", false},
		{"with-dash", "foo-bar", false},
		{"with-dot", "foo.txt", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"forward-slash", "foo/bar", true},
		{"backslash", "foo\\bar", true},
		{"null-byte", "foo\x00bar", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := SafePathComponent(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafePathComponent(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestSafeJoin(t *testing.T) {
	t.Parallel()

	t.Run("valid child", func(t *testing.T) {
		t.Parallel()
		got, err := SafeJoin("/tmp/base", "child")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/tmp/base/child" {
			t.Errorf("got %q, want %q", got, "/tmp/base/child")
		}
	})

	t.Run("valid nested", func(t *testing.T) {
		t.Parallel()
		got, err := SafeJoin("/tmp/base", "a", "b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/tmp/base/a/b" {
			t.Errorf("got %q, want %q", got, "/tmp/base/a/b")
		}
	})

	t.Run("dotdot escape", func(t *testing.T) {
		t.Parallel()
		_, err := SafeJoin("/tmp/base", "..", "etc", "passwd")
		if err == nil {
			t.Fatal("expected error for path traversal, got nil")
		}
		if !strings.Contains(err.Error(), "escapes base") {
			t.Errorf("expected 'escapes base' error, got: %v", err)
		}
	})

	t.Run("dotdot resolved within", func(t *testing.T) {
		t.Parallel()
		// /tmp/base/a/../b resolves to /tmp/base/b — still within base
		got, err := SafeJoin("/tmp/base", "a", "..", "b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/tmp/base/b" {
			t.Errorf("got %q, want %q", got, "/tmp/base/b")
		}
	})

	t.Run("base only", func(t *testing.T) {
		t.Parallel()
		got, err := SafeJoin("/tmp/base")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/tmp/base" {
			t.Errorf("got %q, want %q", got, "/tmp/base")
		}
	})
}

func TestSafeAbsolutePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string // substring expected in error message, if any
	}{
		{"valid absolute", "/usr/local/bin/grew", false, ""},
		{"valid nested", "/opt/grew/Cellar/jq/1.7", false, ""},
		{"empty", "", true, "empty path"},
		{"relative", "bin/grew", true, "must be absolute"},
		{"dot-relative", "./bin/grew", true, "must be absolute"},
		{"root slash", "/", true, "filesystem root"},
		{"trailing slash", "/usr/local/", true, "traversal or redundant"},
		{"double slash", "/usr//local", true, "traversal or redundant"},
		{"dotdot traversal", "/usr/local/../etc", true, "traversal or redundant"},
		{"dot segment", "/usr/./local", true, "traversal or redundant"},
		{"just dotdot", "..", true, "must be absolute"},
		{"just dot", ".", true, "must be absolute"},
		{"single component", "/grew", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := SafeAbsolutePath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeAbsolutePath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("SafeAbsolutePath(%q) error = %q, want substring %q", tt.input, err.Error(), tt.errMsg)
			}
		})
	}
}

func TestIsSubpath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		base   string
		target string
		want   bool
	}{
		{"equal", "/tmp", "/tmp", true},
		{"child", "/tmp", "/tmp/foo", true},
		{"nested child", "/tmp", "/tmp/foo/bar", true},
		{"outside", "/tmp", "/etc", false},
		{"outside parent", "/tmp/foo", "/tmp", false},
		{"prefix trick", "/tmp/foo", "/tmp/foo2", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsSubpath(tt.base, tt.target); got != tt.want {
				t.Errorf("IsSubpath(%q, %q) = %v, want %v", tt.base, tt.target, got, tt.want)
			}
		})
	}
}

func TestURLExt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/foo.tar.gz", ".tar.gz"},
		{"https://example.com/foo.zip", ".zip"},
		{"https://example.com/foo.tar.xz", ".tar.xz"},
		{"https://example.com/foo", ""},
		{"https://example.com/foo.txt", ".txt"},
		{"invalid url %%", ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := URLExt(tt.url); got != tt.want {
				t.Errorf("URLExt(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestNormalizeDir(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	
	// Create a subdirectory to test symlink resolution
	sub := filepath.Join(tmp, "sub")
	os.Mkdir(sub, 0755)
	
	link := filepath.Join(tmp, "link")
	os.Symlink(sub, link)

	tests := []struct {
		name    string
		dir     string
		kind    string
		wantErr bool
	}{
		{"valid", sub, "test", false},
		{"valid symlink", link, "test", false},
		{"non-existent", filepath.Join(tmp, "nope"), "test", false}, // Existence not required for normalization
		{"empty", "", "test", true},
		{"relative", "relative", "test", true},
		{"root", "/", "test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeDir(tt.dir, tt.kind)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeDir() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				// NormalizeDir should return an absolute, cleaned path.
				if !filepath.IsAbs(got) {
					t.Errorf("NormalizeDir() returned non-absolute path: %q", got)
				}
				// If it was a symlink, it should be resolved.
				if tt.name == "valid symlink" {
					resolvedSub, _ := filepath.EvalSymlinks(sub)
					if got != resolvedSub {
						t.Errorf("NormalizeDir() did not resolve symlink: got %q, want %q", got, resolvedSub)
					}
				}
			}
		})
	}
}
