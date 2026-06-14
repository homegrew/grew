package formula

import "testing"

func TestIsVersioned(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"node@24", true},
		{"python@3.12", true},
		{"gcc@13", true},
		{"node", false},
		{"python", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Formula{Name: tt.name}
			if got := f.IsVersioned(); got != tt.want {
				t.Errorf("IsVersioned() for %q = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestEffectiveKegOnly(t *testing.T) {
	tests := []struct {
		desc    string
		name    string
		kegOnly bool
		want    bool
	}{
		{"unversioned, not keg_only", "node", false, false},
		{"unversioned, explicit keg_only honored", "node", true, true},
		{"versioned implies keg-only even when keg_only false", "node@24", false, true},
		{"versioned and keg_only", "node@24", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := &Formula{Name: tt.name, KegOnly: tt.kegOnly}
			if got := f.EffectiveKegOnly(); got != tt.want {
				t.Errorf("EffectiveKegOnly() for {Name:%q KegOnly:%v} = %v, want %v",
					tt.name, tt.kegOnly, got, tt.want)
			}
		})
	}
}

func TestBaseName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"node@24", "node"},
		{"python@3.12", "python"},
		{"node", "node"},
		{"a@b@c", "a"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := BaseName(tt.in); got != tt.want {
				t.Errorf("BaseName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
