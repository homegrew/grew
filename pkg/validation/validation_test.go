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
		{"with-plus", "c++", true},
		{"starts-with-plus", "+foo", false},
		{"with-underscore", "my_pkg", true},
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
