package validation

import (
	"strings"
	"testing"
)

func TestIsValidName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple", "jq", true},
		{"with-dash", "go-task", true},
		{"with-dot", "node.js", true},
		{"with-at", "php@8.1", true},
		{"with-plus", "c++", true},           // + is in the allowed char class
		{"starts-with-plus", "+foo", false},  // invalid start char
		{"with-underscore", "my_pkg", true},  // underscore matches \-\+ class? No, _ matches [a-z0-9@._\-\+]
		{"uppercase", "Jq", false},
		{"empty", "", false},
		{"dot-dot", "..", false},
		{"slash", "foo/bar", false},
		{"backslash", "foo\\bar", false},
		{"starts-with-dot", ".hidden", false},
		{"starts-with-dash", "-flag", false},
		{"single-char", "a", true},
		{"single-digit", "0", true},
		{"number", "7zip", true},
		{"complex-valid", "lib2to3-extras", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidName(tt.input); got != tt.want {
				t.Errorf("IsValidName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"semver", "1.2.3", true},
		{"with-dash", "1.0.0-rc1", true},
		{"with-plus", "1.0+build", true},
		{"with-tilde", "1.0~beta", true},
		{"with-colon", "2:1.0", true},
		{"uppercase", "V1.0", true},
		{"single-digit", "0", true},
		{"empty", "", false},
		{"dot-dot", "..", false},
		{"starts-with-dot", ".1", false},
		{"starts-with-dash", "-1", false},
		{"slash", "1/2", false},
		{"backslash", "1\\2", false},
		{"spaces", "1 2", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidVersion(tt.input); got != tt.want {
				t.Errorf("IsValidVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

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

func TestValidateSHA256(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", false},
		{"valid-uppercase", "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855", false},
		{"too-short", "e3b0c44298fc1c149afbf4c8996fb924", true},
		{"too-long", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85500", true},
		{"empty", "", true},
		{"not-hex", "g3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"64-chars-not-hex", strings.Repeat("zz", 32), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSHA256(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSHA256(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
