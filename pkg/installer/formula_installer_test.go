package installer

import (
	"reflect"
	"testing"
)

func TestExpandBuildVars(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		prefix string
		want   []string
	}{
		{
			name:   "substitutes prefix placeholder",
			args:   []string{"./configure", "--prefix={prefix}", "--enable-threads"},
			prefix: "/opt/homegrew/Cellar/tcl-tk/9.0.3",
			want:   []string{"./configure", "--prefix=/opt/homegrew/Cellar/tcl-tk/9.0.3", "--enable-threads"},
		},
		{
			name:   "leaves args without placeholder unchanged",
			args:   []string{"make", "install"},
			prefix: "/opt/homegrew/Cellar/foo/1.0",
			want:   []string{"make", "install"},
		},
		{
			name:   "substitutes multiple occurrences in one arg",
			args:   []string{"DESTDIR={prefix}", "PREFIX={prefix}"},
			prefix: "/keg",
			want:   []string{"DESTDIR=/keg", "PREFIX=/keg"},
		},
		{
			name:   "empty input yields empty output",
			args:   []string{},
			prefix: "/keg",
			want:   []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandBuildVars(tc.args, tc.prefix)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expandBuildVars(%v, %q) = %v, want %v", tc.args, tc.prefix, got, tc.want)
			}
		})
	}
}
