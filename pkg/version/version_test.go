package version

import "testing"

func TestCompare(t *testing.T) {
	t.Parallel()
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
		tt := tt
		t.Run(tt.v1+"_"+tt.v2, func(t *testing.T) {
			t.Parallel()
			if got := Compare(tt.v1, tt.v2); got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		old, new string
		want     bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0-rc1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc1", false},
		{"", "v1.0.0", true},
		{"v1.0.0", "", false},
		{"v1.a.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.$", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.old+"_"+tt.new, func(t *testing.T) {
			t.Parallel()
			if got := IsNewer(tt.old, tt.new); got != tt.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.old, tt.new, got, tt.want)
			}
		})
	}
}
