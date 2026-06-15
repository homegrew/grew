package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/homegrew/grew/pkg/homebrew"
)

// TestLive_TclTkBuildOverride fetches tcl-tk from the live Homebrew API, runs it
// through the real conversion + override path, and confirms the generated YAML
// carries the build.working_dir stanza needed to build from source.
//
// It hits the network, so it is gated behind GREW_LIVE_API=1 and skipped in the
// normal unit run / CI:
//
//	GREW_LIVE_API=1 go test -run TestLive_TclTkBuildOverride ./tools/genrepo/
func TestLive_TclTkBuildOverride(t *testing.T) {
	if os.Getenv("GREW_LIVE_API") != "1" {
		t.Skip("set GREW_LIVE_API=1 to run live Homebrew API checks")
	}

	f, err := homebrew.FetchFormula("tcl-tk")
	if err != nil {
		t.Fatalf("fetch tcl-tk from Homebrew API: %v", err)
	}

	// Sanity: the conversion must already supply a source archive, otherwise a
	// working_dir alone wouldn't make `-s` work.
	if f.Source == nil || f.Source.URL == "" {
		t.Fatalf("expected tcl-tk to carry a source URL from conversion, got %+v", f.Source)
	}

	applyFormulaOverrides(f)

	if f.Build.WorkingDir != "unix" {
		t.Fatalf("expected build.working_dir %q after override, got %q", "unix", f.Build.WorkingDir)
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("overridden tcl-tk failed validation: %v", err)
	}

	data, err := yaml.Marshal(f)
	if err != nil {
		t.Fatalf("marshal tcl-tk: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "working_dir: unix") {
		t.Fatalf("emitted YAML missing build.working_dir:\n%s", out)
	}
	if !strings.Contains(out, "source:") {
		t.Fatalf("emitted YAML missing source section:\n%s", out)
	}
	t.Logf("live tcl-tk YAML (%d bytes) emitted with build.working_dir=unix and source section", len(data))
	t.Logf("build/source stanzas:\n%s", excerptStanzas(out, "build:", "source:"))
}

// excerptStanzas returns the lines of top-level YAML blocks whose key matches
// any of headers, for compact test logging.
func excerptStanzas(yaml string, headers ...string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(yaml, "\n") {
		topLevel := line != "" && line[0] != ' ' && line[0] != '\t'
		if topLevel {
			in = false
			for _, h := range headers {
				if strings.HasPrefix(line, h) {
					in = true
					break
				}
			}
		}
		if in {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
