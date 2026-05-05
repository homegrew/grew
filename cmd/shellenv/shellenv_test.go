package shellenv

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunShellenv(t *testing.T) {
	t.Setenv("HOMEGREW_NO_INIT_TAP", "1")
	t.Setenv("HOMEGREW_PREFIX", t.TempDir())
	
	tests := []struct {
		name     string
		args     []string
		contains []string
		// if set, at least one of these must be present (used for PATH vs path_helper)
		alternatives []string
	}{
		{
			name: "bash",
			args: []string{"bash"},
			contains: []string{
				"export HOMEGREW_PREFIX=",
				"export HOMEGREW_CELLAR=",
				"export HOMEGREW_REPOSITORY=",
				"export INFOPATH=",
			},
			alternatives: []string{"export PATH=", "path_helper"},
		},
		{
			name: "zsh",
			args: []string{"zsh"},
			contains: []string{
				"export HOMEGREW_PREFIX=",
				"fpath[1,0]=",
				"export FPATH;",
			},
			alternatives: []string{"export PATH=", "path_helper"},
		},
		{
			name: "fish",
			args: []string{"fish"},
			contains: []string{
				"set --global --export HOMEGREW_PREFIX ",
				"fish_add_path --global --move --path ",
				"if test -n \"$MANPATH[1]\"; set --global --export MANPATH '' $MANPATH; end;",
			},
		},
		{
			name: "pwsh",
			args: []string{"pwsh"},
			contains: []string{
				"[System.Environment]::SetEnvironmentVariable('HOMEGREW_PREFIX',",
				"[System.Environment]::SetEnvironmentVariable('PATH',",
			},
		},
		{
			name: "csh",
			args: []string{"csh"},
			contains: []string{
				"setenv HOMEGREW_PREFIX ",
			},
			alternatives: []string{"setenv PATH ", "path_helper"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Prepend the subcommand name so the root command routes it correctly
			root := &cobra.Command{Use: "grew"}
			root.AddCommand(Command)

			args := append([]string{"shellenv"}, tt.args...)
			root.SetArgs(args)
			err := root.Execute()

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

			if len(tt.alternatives) > 0 {
				found := false
				for _, alt := range tt.alternatives {
					if strings.Contains(got, alt) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("runShellenv() output missing any of %v, got:\n%s", tt.alternatives, got)
				}
			}
		})
	}
}
