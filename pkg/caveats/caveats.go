// Package caveats renders post-install messages for formulas. Output goes
// through pkg/ui conventions and is produced only after successful installation.
package caveats

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/homegrew/grew/pkg/formula"
	"github.com/homegrew/grew/pkg/ui"
)

// Renderer writes formula caveats to a writer using pkg/ui conventions.
type Renderer struct {
	w io.Writer
}

// New returns a Renderer that writes to w.
func New(w io.Writer) *Renderer {
	return &Renderer{w: w}
}

// templateData is the data passed to the caveats template.
type templateData struct {
	Formula string
	Version string
	Prefix  string
}

// Render prints f.Caveats to the writer after applying template substitution
// for {{.Formula}}, {{.Version}}, and {{.Prefix}}. It is a no-op when
// f.Caveats is empty. Returns an error if f.Caveats contains an http:// URL
// or if template parsing or execution fails.
func (r *Renderer) Render(f formula.Formula, prefix string) error {
	if f.Caveats == "" {
		return nil
	}
	if strings.Contains(f.Caveats, "http://") {
		return fmt.Errorf("caveats for %q contain an insecure http:// URL", f.Name)
	}

	tmpl, err := template.New("caveats").Parse(f.Caveats)
	if err != nil {
		return fmt.Errorf("parse caveats template for %q: %w", f.Name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData{
		Formula: f.Name,
		Version: f.Version,
		Prefix:  prefix,
	}); err != nil {
		return fmt.Errorf("render caveats for %q: %w", f.Name, err)
	}

	ui.FprintArrow(r.w, "Caveats")
	fmt.Fprintln(r.w, buf.String())
	return nil
}
