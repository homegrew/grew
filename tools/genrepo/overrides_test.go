package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/homegrew/grew/pkg/formula"
)

func TestApplyFormulaOverrides_SetsWorkingDir(t *testing.T) {
	f := &formula.Formula{Name: "tcl-tk"}
	applyFormulaOverrides(f)
	if f.Build.WorkingDir != "unix" {
		t.Fatalf("expected build.working_dir %q for tcl-tk, got %q", "unix", f.Build.WorkingDir)
	}
}

func TestApplyFormulaOverrides_UnknownFormulaUntouched(t *testing.T) {
	f := &formula.Formula{Name: "jq"}
	applyFormulaOverrides(f)
	if f.Build.WorkingDir != "" || len(f.Build.Configure) != 0 || len(f.Build.Install) != 0 {
		t.Fatalf("expected jq build config untouched, got %+v", f.Build)
	}
}

func TestApplyFormulaOverrides_DoesNotClobberExisting(t *testing.T) {
	f := &formula.Formula{
		Name:  "tcl-tk",
		Build: formula.BuildSpec{WorkingDir: "custom"},
	}
	applyFormulaOverrides(f)
	if f.Build.WorkingDir != "custom" {
		t.Fatalf("expected existing build.working_dir preserved, got %q", f.Build.WorkingDir)
	}
}

// TestApplyFormulaOverrides_EmitsYAML exercises the marshal path used by
// saveYAML, confirming the override surfaces as a build.working_dir stanza in
// the generated formula definition.
func TestApplyFormulaOverrides_EmitsYAML(t *testing.T) {
	f := &formula.Formula{
		Name:    "tcl-tk",
		Version: "9.0.3",
		Source:  &formula.SourceSpec{URL: "https://example.com/tcl9.0.3-src.tar.gz", SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	applyFormulaOverrides(f)
	data, err := yaml.Marshal(f)
	if err != nil {
		t.Fatalf("marshal overridden formula: %v", err)
	}
	if !strings.Contains(string(data), "working_dir: unix") {
		t.Fatalf("expected emitted YAML to contain build.working_dir, got:\n%s", data)
	}
}
