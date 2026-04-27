package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunShellenv(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		contains []string
	}{
		{
			name: "bash",
			args: []string{"bash"},
			contains: []string{
				"export HOMEGREW_PREFIX=",
				"export HOMEGREW_CELLAR=",
				"export HOMEGREW_REPOSITORY=",
				"export PATH=",
				"export MANPATH=",
				"export INFOPATH=",
			},
		},
		{
			name: "zsh",
			args: []string{"zsh"},
			contains: []string{
				"export HOMEGREW_PREFIX=",
				"export PATH=",
				"fpath=(",
			},
		},
		{
			name: "fish",
			args: []string{"fish"},
			contains: []string{
				"set -gx HOMEGREW_PREFIX ",
				"set -gx PATH ",
				"set -q MANPATH",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := runShellenv(tt.args)
			w.Close()
			os.Stdout = oldStdout

			if err != nil {
				t.Fatalf("runShellenv() error = %v", err)
			}

			var buf bytes.Buffer
			io.Copy(&buf, r)
			got := buf.String()

			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("runShellenv() output missing %q, got:\n%s", want, got)
				}
			}
		})
	}
}
