package caveats

import (
	"bytes"
	"strings"
	"testing"

	"github.com/homegrew/grew/pkg/formula"
)

func makeFormula(name, version, caveats string) formula.Formula {
	return formula.Formula{
		Name:    name,
		Version: version,
		Caveats: caveats,
	}
}

func TestRender_Empty_NoOutput(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := New(&buf)
	if err := r.Render(makeFormula("pkg", "1.0", ""), "/opt/homegrew"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty caveats, got %q", buf.String())
	}
}

func TestRender_NonEmpty_ContainsCaveats(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := New(&buf)
	if err := r.Render(makeFormula("mypkg", "2.0", "Remember to update PATH."), "/opt/homegrew"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Caveats") {
		t.Errorf("output should contain 'Caveats', got: %q", out)
	}
	if !strings.Contains(out, "Remember to update PATH.") {
		t.Errorf("output should contain the caveats text, got: %q", out)
	}
}

func TestRender_TemplateSubstitution(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := New(&buf)
	tmpl := "{{.Formula}} {{.Version}} installed to {{.Prefix}}/bin"
	if err := r.Render(makeFormula("jq", "1.7", tmpl), "/opt/homegrew"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "jq 1.7 installed to /opt/homegrew/bin") {
		t.Errorf("template not substituted correctly, got: %q", out)
	}
}

func TestRender_HTTPUrl_ReturnsError(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := New(&buf)
	if err := r.Render(makeFormula("pkg", "1.0", "See http://example.com for docs."), "/prefix"); err == nil {
		t.Fatal("expected error for http:// URL in caveats")
	}
	if buf.Len() != 0 {
		t.Error("no output should be produced when returning an error")
	}
}

func TestRender_BadTemplate_ReturnsError(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := New(&buf)
	if err := r.Render(makeFormula("pkg", "1.0", "{{.Missing .Broken}}"), "/prefix"); err == nil {
		t.Fatal("expected error for malformed template")
	}
}
