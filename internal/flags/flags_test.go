package flags

import (
	"flag"
	"io"
	"testing"
)

func reset() {
	Verbose = false
	Debug = false
}

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantArgs    []string
		wantVerbose bool
		wantDebug   bool
	}{
		{
			name:        "no flags",
			args:        []string{"install", "jq"},
			wantArgs:    []string{"install", "jq"},
			wantVerbose: false,
			wantDebug:   false,
		},
		{
			name:        "verbose short",
			args:        []string{"-v", "install", "jq"},
			wantArgs:    []string{"install", "jq"},
			wantVerbose: true,
			wantDebug:   false,
		},
		{
			name:        "verbose long",
			args:        []string{"--verbose", "install", "jq"},
			wantArgs:    []string{"install", "jq"},
			wantVerbose: true,
			wantDebug:   false,
		},
		{
			name:        "debug short",
			args:        []string{"-d", "install", "jq"},
			wantArgs:    []string{"install", "jq"},
			wantVerbose: false, // Resolve handles debug implies verbose
			wantDebug:   true,
		},
		{
			name:        "debug long",
			args:        []string{"--debug", "install", "jq"},
			wantArgs:    []string{"install", "jq"},
			wantVerbose: false,
			wantDebug:   true,
		},
		{
			name:        "mixed flags",
			args:        []string{"-v", "-d", "install", "jq"},
			wantArgs:    []string{"install", "jq"},
			wantVerbose: true,
			wantDebug:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reset()
			got := Parse(tt.args)
			if Verbose != tt.wantVerbose {
				t.Errorf("Verbose = %v, want %v", Verbose, tt.wantVerbose)
			}
			if Debug != tt.wantDebug {
				t.Errorf("Debug = %v, want %v", Debug, tt.wantDebug)
			}
			if len(got) != len(tt.wantArgs) {
				t.Fatalf("got args %v, want %v", got, tt.wantArgs)
			}
			for i := range got {
				if got[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, got[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantVerbose bool
		wantDebug   bool
	}{
		{
			name:        "no flags",
			args:        []string{"jq"},
			wantVerbose: false,
			wantDebug:   false,
		},
		{
			name:        "verbose short",
			args:        []string{"-v", "jq"},
			wantVerbose: true,
			wantDebug:   false,
		},
		{
			name:        "debug long",
			args:        []string{"--debug", "jq"},
			wantVerbose: false,
			wantDebug:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reset()
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			Register(fs)
			if err := fs.Parse(tt.args); err != nil {
				t.Fatalf("fs.Parse failed: %v", err)
			}
			if Verbose != tt.wantVerbose {
				t.Errorf("Verbose = %v, want %v", Verbose, tt.wantVerbose)
			}
			if Debug != tt.wantDebug {
				t.Errorf("Debug = %v, want %v", Debug, tt.wantDebug)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		initialVerb bool
		initialDeb  bool
		wantVerbose bool
	}{
		{
			name:        "no flags",
			initialVerb: false,
			initialDeb:  false,
			wantVerbose: false,
		},
		{
			name:        "verbose only",
			initialVerb: true,
			initialDeb:  false,
			wantVerbose: true,
		},
		{
			name:        "debug only",
			initialVerb: false,
			initialDeb:  true,
			wantVerbose: true,
		},
		{
			name:        "both",
			initialVerb: true,
			initialDeb:  true,
			wantVerbose: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Verbose = tt.initialVerb
			Debug = tt.initialDeb
			Resolve()
			if Verbose != tt.wantVerbose {
				t.Errorf("after Resolve, Verbose = %v, want %v", Verbose, tt.wantVerbose)
			}
		})
	}
}
