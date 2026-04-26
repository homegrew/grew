package version

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"1.0.0", "v1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.1.0", "v1.0.0", 1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0-rc1", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-rc1", 1},
		{"v1.0.0-rc1", "v1.0.0-rc2", -1},
		{"v0.0.0-UNKNOWN", "v0.1.0", -1},
		{"", "v1.0.0", -1},
		{"v", "v1.0.0", -1},
		{"v1.a.0", "v1.0.0", 0},
		{"v1.0.$", "v1.0.0", 0},
		{"v1.0.0-??", "v1.0.0", -1},
	}

	for _, tt := range tests {
		if got := Compare(tt.v1, tt.v2); got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("v1.0.0", "v1.0.1") {
		t.Error("expected second argument (v1.0.1) to be newer than first argument (v1.0.0)")
	}
	if IsNewer("v1.0.1", "v1.0.0") {
		t.Error("expected v1.0.0 NOT to be newer than v1.0.1")
	}
}
