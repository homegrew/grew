package desc

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/pkg/context"
)

// resetFlags restores all package-level flag state to its zero value so tests
// (which mutate these globals) do not leak into one another.
func resetFlags() {
}

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

// setupCtx builds a hermetic context backed by a temp prefix containing a core
// tap (formulae) and a cask tap, each pre-populated with the given YAML files.
// fileMap maps a relative path under the tap root (e.g. "core/jq.yaml") to its
// YAML contents.
func setupCtx(t *testing.T, files map[string]string) *context.Context {
	t.Helper()
	tmpDir := t.TempDir()

	for _, kv := range []struct{ k, v string }{
		{"HOMEGREW_PREFIX", tmpDir},
		{"HOMEGREW_CACHE", filepath.Join(tmpDir, "cache")},
		{"HOMEGREW_APPDIR", filepath.Join(tmpDir, "Applications")},
	} {
		t.Setenv(kv.k, kv.v)
	}

	tapRoot := filepath.Join(tmpDir, "Taps", "homegrew", "homegrew-taps")
	// .git so context.New() does not attempt to clone the core tap.
	if err := os.MkdirAll(filepath.Join(tapRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Ensure both tap subdirs exist even if no files target them.
	for _, d := range []string{"core", "cask"} {
		if err := os.MkdirAll(filepath.Join(tapRoot, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	for rel, body := range files {
		full := filepath.Join(tapRoot, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, err := context.New()
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func formulaYAML(name, desc string) string {
	return `name: ` + name + `
version: 1.0.0
description: ` + desc + `
homepage: https://example.com
license: MIT
url:
  darwin_arm64: https://example.com/` + name + `-1.0.0.tar.gz
sha256:
  darwin_arm64: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
install:
  type: binary
`
}

func caskYAML(name, desc string) string {
	return `name: ` + name + `
version: 2.0.0
description: ` + desc + `
homepage: https://example.com/cask
url:
  darwin_arm64: https://example.com/` + name + `-2.0.0.zip
sha256:
  darwin_arm64: fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
artifacts:
  app:
    - Test.app
`
}

// ---- Pure helper tests (no context needed) ----

func TestMatcherLiteralSubstring(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ms, err := buildMatchers([]string{"JSON"})
	if err != nil {
		t.Fatal(err)
	}
	// case-insensitive substring against name
	if !matchAny(descOptions{search: true}, ms, "jq", "Lightweight JSON processor") {
		t.Error("expected literal 'JSON' to match description case-insensitively")
	}
	if matchAny(descOptions{search: true}, ms, "wget", "Internet file retriever") {
		t.Error("did not expect 'JSON' to match unrelated package")
	}
}

func TestMatchAnyNameMode(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ms, err := buildMatchers([]string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !matchAny(descOptions{name: true}, ms, "foobar", "unrelated description") {
		t.Error("expected name-mode to match on name")
	}
	if matchAny(descOptions{name: true}, ms, "bar", "contains foo in description") {
		t.Error("name-mode must not match on description")
	}
}

func TestMatchAnyDescriptionMode(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ms, err := buildMatchers([]string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !matchAny(descOptions{description: true}, ms, "bar", "this has foo inside") {
		t.Error("expected description-mode to match on description")
	}
	if matchAny(descOptions{description: true}, ms, "foobar", "unrelated") {
		t.Error("description-mode must not match on name")
	}
}

func TestRegexPatternMatching(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ms, err := buildMatchers([]string{"/^py.*3$/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].re == nil {
		t.Fatalf("expected a compiled regex matcher, got %+v", ms)
	}
	if !matchAny(descOptions{name: true}, ms, "Python3", "lang") {
		t.Error("expected regex /^py.*3$/ to match 'Python3' case-insensitively")
	}
	if matchAny(descOptions{name: true}, ms, "ruby", "lang") {
		t.Error("did not expect regex to match 'ruby'")
	}
}

func TestRegexTooLongRejected(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()
	long := "/" + strings.Repeat("a", maxPatternLen+1) + "/"
	_, err := buildMatchers([]string{long})
	if err == nil {
		t.Fatal("expected error for over-long regex pattern")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got: %v", err)
	}
}

func TestRegexInvalidRejected(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()
	_, err := buildMatchers([]string{"/(unterminated/"})
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("expected 'invalid regex' error, got: %v", err)
	}
}

func TestDedupe(t *testing.T) {
	in := []entry{{name: "a"}, {name: "b"}, {name: "a"}, {name: "c"}, {name: "b"}}
	out := dedupe(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 unique entries, got %d: %+v", len(out), out)
	}
}

// ---- render / output-format tests ----

func TestRenderGroupedHeaders(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags() // descPlain = false -> grouped

	out := captureStdout(t, func() {
		render(
			descOptions{plain: false},
			[]entry{{name: "jq", desc: "json"}},
			[]entry{{name: "firefox", desc: "browser"}},
		)
	})

	if !strings.Contains(out, "==> Formulae") {
		t.Errorf("expected ==> Formulae header, got:\n%s", out)
	}
	if !strings.Contains(out, "==> Casks") {
		t.Errorf("expected ==> Casks header, got:\n%s", out)
	}
	if !strings.Contains(out, "jq: json") {
		t.Errorf("expected 'jq: json' line, got:\n%s", out)
	}
	if !strings.Contains(out, "firefox: browser") {
		t.Errorf("expected 'firefox: browser' line, got:\n%s", out)
	}
}

func TestRenderGroupedOmitsEmptyHeader(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	out := captureStdout(t, func() {
		render(descOptions{plain: false}, []entry{{name: "jq", desc: "json"}}, nil)
	})
	if strings.Contains(out, "==> Casks") {
		t.Errorf("did not expect Casks header when no casks, got:\n%s", out)
	}
	if !strings.Contains(out, "==> Formulae") {
		t.Errorf("expected Formulae header, got:\n%s", out)
	}
}

func TestRenderPlainNoHeaders(t *testing.T) {
	defer resetFlags()
	resetFlags()

	out := captureStdout(t, func() {
		render(
			descOptions{plain: true},
			[]entry{{name: "jq", desc: "json"}},
			[]entry{{name: "firefox", desc: "browser"}},
		)
	})

	if strings.Contains(out, "==>") {
		t.Errorf("plain mode must not emit ==> headers, got:\n%s", out)
	}
	if !strings.Contains(out, "jq: json") || !strings.Contains(out, "firefox: browser") {
		t.Errorf("plain mode must still emit name: desc lines, got:\n%s", out)
	}
}

func TestRenderSortsByName(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	out := captureStdout(t, func() {
		render(descOptions{plain: true}, []entry{{name: "zed", desc: "z"}, {name: "abc", desc: "a"}}, nil)
	})
	ai := strings.Index(out, "abc:")
	zi := strings.Index(out, "zed:")
	if ai == -1 || zi == -1 || ai > zi {
		t.Errorf("expected abc before zed, got:\n%s", out)
	}
}

// ---- context-backed mode tests ----

func TestRunNameMode(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"core/jq.yaml": formulaYAML("jq", "Lightweight JSON processor"),
	})

	out := captureStdout(t, func() {
		if err := runNameMode(ctx, descOptions{plain: false}, []string{"jq"}); err != nil {
			t.Fatalf("runNameMode failed: %v", err)
		}
	})

	if !strings.Contains(out, "==> Formulae") {
		t.Errorf("expected Formulae header, got:\n%s", out)
	}
	if !strings.Contains(out, "jq: Lightweight JSON processor") {
		t.Errorf("expected 'jq: Lightweight JSON processor', got:\n%s", out)
	}
}

func TestRunNameModeMissingPackage(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"core/jq.yaml": formulaYAML("jq", "Lightweight JSON processor"),
	})

	out := captureStdout(t, func() {
		err := runNameMode(ctx, descOptions{plain: false}, []string{"jq", "does-not-exist"})
		if err == nil {
			t.Error("expected non-nil error when a package is missing")
		}
	})
	// the found package should still print
	if !strings.Contains(out, "jq: Lightweight JSON processor") {
		t.Errorf("expected found package to still print, got:\n%s", out)
	}
}

func TestRunSearchModeSubstring(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"core/jq.yaml":      formulaYAML("jq", "Lightweight JSON processor"),
		"core/wget.yaml":    formulaYAML("wget", "Internet file retriever"),
		"cask/firefox.yaml": caskYAML("firefox", "Web browser"),
	})

	out := captureStdout(t, func() {
		if err := runSearchMode(ctx, descOptions{search: true}, []string{"json"}); err != nil {
			t.Fatalf("runSearchMode failed: %v", err)
		}
	})

	if !strings.Contains(out, "jq: Lightweight JSON processor") {
		t.Errorf("expected jq to match 'json', got:\n%s", out)
	}
	if strings.Contains(out, "wget:") {
		t.Errorf("did not expect wget to match 'json', got:\n%s", out)
	}
}

// Note on isolation: both the formula and cask loaders recursively scan the
// shared Taps/ tree, and a valid cask YAML (artifacts.app) also validates as a
// formula. So a single repo containing both kinds cross-contaminates the two
// loaders. To test the --formula/--cask gating cleanly, each restriction test
// populates only the kind it asserts on, which lets us assert that the gated
// loader is never consulted (no opposite-group header).

func TestRunSearchModeFormulaRestriction(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"core/fooform.yaml": formulaYAML("fooform", "a foo formula"),
	})

	out := captureStdout(t, func() {
		if err := runSearchMode(ctx, descOptions{formula: true}, []string{"foo"}); err != nil {
			t.Fatalf("runSearchMode failed: %v", err)
		}
	})

	if !strings.Contains(out, "fooform:") {
		t.Errorf("expected formula fooform in --formula results, got:\n%s", out)
	}
	if strings.Contains(out, "==> Casks") {
		t.Errorf("--formula must not print Casks header, got:\n%s", out)
	}
}

func TestRunSearchModeCaskRestriction(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"cask/foocask.yaml": caskYAML("foocask", "a foo cask"),
	})

	out := captureStdout(t, func() {
		if err := runSearchMode(ctx, descOptions{cask: true}, []string{"foo"}); err != nil {
			t.Fatalf("runSearchMode failed: %v", err)
		}
	})

	if !strings.Contains(out, "foocask:") {
		t.Errorf("expected cask foocask in --cask results, got:\n%s", out)
	}
	if strings.Contains(out, "==> Formulae") {
		t.Errorf("--cask must not print Formulae header, got:\n%s", out)
	}
}

func TestRunSearchModeRegex(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"core/python3.yaml": formulaYAML("python3", "interpreted language"),
		"core/ruby.yaml":    formulaYAML("ruby", "interpreted language"),
	})

	out := captureStdout(t, func() {
		if err := runSearchMode(ctx, descOptions{name: true}, []string{"/^py.*3$/"}); err != nil {
			t.Fatalf("runSearchMode failed: %v", err)
		}
	})

	if !strings.Contains(out, "python3:") {
		t.Errorf("expected python3 to match regex, got:\n%s", out)
	}
	if strings.Contains(out, "ruby:") {
		t.Errorf("did not expect ruby to match regex, got:\n%s", out)
	}
}

func TestRunDescMutualExclusion(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"core/python3.yaml": formulaYAML("python3", "interpreted language"),
		"core/ruby.yaml":    formulaYAML("ruby", "interpreted language"),
	})
	if err := runDesc(ctx, descOptions{search: true, name: true}, []string{"foo"}); err == nil {
		t.Error("expected error when multiple search modes are set")
	}
}

func TestRunDescFormulaCaskExclusion(t *testing.T) {
	t.Cleanup(resetFlags)
	resetFlags()

	ctx := setupCtx(t, map[string]string{
		"cask/foocask.yaml": caskYAML("foocask", "a foo cask"),
	})

	if err := runDesc(ctx, descOptions{formula: true, cask: true}, []string{"foo"}); err == nil {
		t.Error("expected error when both --formula and --cask are set")
	}
}
