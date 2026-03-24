package linkage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyAbsPath_System(t *testing.T) {
	t.Parallel()
	dep := classifyAbsPath("/usr/lib/libSystem.B.dylib", "/tmp/cellar/foo/1.0", "/tmp/cellar")
	if dep.Kind != System {
		t.Errorf("expected System, got %v", dep.Kind)
	}
}

func TestClassifyAbsPath_Self(t *testing.T) {
	t.Parallel()
	kegPath := "/tmp/cellar/foo/1.0"
	dep := classifyAbsPath(kegPath+"/lib/libfoo.dylib", kegPath, "/tmp/cellar")
	if dep.Kind != Self {
		t.Errorf("expected Self, got %v", dep.Kind)
	}
}

func TestClassifyAbsPath_OtherKeg(t *testing.T) {
	t.Parallel()
	dep := classifyAbsPath("/tmp/cellar/bar/2.0/lib/libbar.dylib", "/tmp/cellar/foo/1.0", "/tmp/cellar")
	if dep.Kind != OtherKeg {
		t.Errorf("expected OtherKeg, got %v", dep.Kind)
	}
	if dep.Formula != "bar" {
		t.Errorf("expected formula 'bar', got %q", dep.Formula)
	}
}

func TestClassifyAbsPath_Broken(t *testing.T) {
	t.Parallel()
	dep := classifyAbsPath("/nonexistent/path/libfoo.dylib", "/tmp/cellar/foo/1.0", "/tmp/cellar")
	if dep.Kind != Broken {
		t.Errorf("expected Broken, got %v", dep.Kind)
	}
}

func TestClassifyAbsPath_ExistsButNotCellar(t *testing.T) {
	t.Parallel()
	dep := classifyAbsPath("/", "/tmp/cellar/foo/1.0", "/tmp/cellar")
	if dep.Kind != System {
		t.Errorf("expected System for existing non-cellar path, got %v", dep.Kind)
	}
}

func TestIsVariableRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want bool
	}{
		{"@rpath/libfoo.dylib", true},
		{"@loader_path/libfoo.dylib", true},
		{"@executable_path/libfoo.dylib", true},
		{"$ORIGIN/libfoo.so", true},
		{"/usr/lib/libfoo.dylib", false},
		{"libfoo.dylib", false},
	}
	for _, tc := range tests {
		if got := isVariableRef(tc.path); got != tc.want {
			t.Errorf("isVariableRef(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestResolveVariable_LoaderPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	libPath := filepath.Join(dir, "libfoo.dylib")
	os.WriteFile(libPath, []byte("fake"), 0644)

	binaryPath := filepath.Join(dir, "tool")
	resolved := resolveVariable("@loader_path/libfoo.dylib", binaryPath, nil)
	if resolved != libPath {
		t.Errorf("resolved = %q, want %q", resolved, libPath)
	}
}

func TestResolveVariable_Rpath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	libDir := filepath.Join(dir, "lib")
	os.MkdirAll(libDir, 0755)
	libPath := filepath.Join(libDir, "libfoo.dylib")
	os.WriteFile(libPath, []byte("fake"), 0644)

	binaryPath := filepath.Join(dir, "bin", "tool")
	resolved := resolveVariable("@rpath/libfoo.dylib", binaryPath, []string{libDir})
	if resolved != libPath {
		t.Errorf("resolved = %q, want %q", resolved, libPath)
	}
}

func TestResolveVariable_RpathWithLoaderPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	libDir := filepath.Join(dir, "lib")
	os.MkdirAll(binDir, 0755)
	os.MkdirAll(libDir, 0755)
	libPath := filepath.Join(libDir, "libfoo.dylib")
	os.WriteFile(libPath, []byte("fake"), 0644)

	binaryPath := filepath.Join(binDir, "tool")
	resolved := resolveVariable("@rpath/libfoo.dylib", binaryPath, []string{"@loader_path/../lib"})
	if resolved != libPath {
		t.Errorf("resolved = %q, want %q", resolved, libPath)
	}
}

func TestResolveVariable_NotFound(t *testing.T) {
	t.Parallel()
	resolved := resolveVariable("@rpath/nonexistent.dylib", "/tmp/bin/tool", []string{"/tmp/lib"})
	if resolved != "" {
		t.Errorf("expected empty, got %q", resolved)
	}
}

func TestIsBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	tests := []struct {
		name  string
		magic []byte
		want  bool
	}{
		{"macho64-le", []byte{0xCF, 0xFA, 0xED, 0xFE}, true},
		{"elf", []byte{0x7F, 'E', 'L', 'F'}, true},
		{"text", []byte{0x23, 0x21, 0x2F, 0x62}, false},
	}
	for _, tc := range tests {
		path := filepath.Join(dir, tc.name)
		data := make([]byte, 4)
		copy(data, tc.magic)
		os.WriteFile(path, data, 0644)
		if got := isBinary(path); got != tc.want {
			t.Errorf("isBinary(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFormatResult_NoBinaries(t *testing.T) {
	t.Parallel()
	r := &Result{Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0"}
	out := FormatResult(r, FormatOpts{})
	if !strings.Contains(out, "No dynamic libraries") {
		t.Errorf("expected 'No dynamic libraries', got %q", out)
	}
}

func TestFormatResult_TestMode_NoBroken(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/tmp/cellar/foo/1.0/bin/foo", Deps: []Dep{
				{Path: "/usr/lib/libSystem.B.dylib", Kind: System},
			}},
		},
	}
	out := FormatResult(r, FormatOpts{Test: true})
	if !strings.Contains(out, "No broken linkage") {
		t.Errorf("expected 'No broken linkage', got %q", out)
	}
}

func TestFormatResult_TestMode_Broken(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/tmp/cellar/foo/1.0/bin/foo", Deps: []Dep{
				{Path: "/nonexistent/libfoo.dylib", Kind: Broken},
			}},
		},
	}
	out := FormatResult(r, FormatOpts{Test: true})
	if !strings.Contains(out, "Broken linkage") {
		t.Errorf("expected 'Broken linkage', got %q", out)
	}
	if !strings.Contains(out, "/nonexistent/libfoo.dylib") {
		t.Errorf("expected broken path in output, got %q", out)
	}
}

func TestBroken(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/usr/lib/ok.dylib", Kind: System},
				{Path: "/missing.dylib", Kind: Broken},
			}},
			{Path: "/bin/b", Deps: []Dep{
				{Path: "/also/missing.dylib", Kind: Broken},
			}},
		},
	}
	broken := r.Broken()
	if len(broken) != 2 {
		t.Errorf("expected 2 broken, got %d", len(broken))
	}
}

func TestLinkedFormulas(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/usr/lib/ok.dylib", Kind: System},
				{Path: "/cellar/bar/1.0/lib/libbar.dylib", Kind: OtherKeg, Formula: "bar"},
				{Path: "/cellar/baz/2.0/lib/libbaz.dylib", Kind: OtherKeg, Formula: "baz"},
			}},
			{Path: "/bin/b", Deps: []Dep{
				{Path: "/cellar/bar/1.0/lib/libbar.dylib", Kind: OtherKeg, Formula: "bar"},
			}},
		},
	}
	linked := r.LinkedFormulas()
	if len(linked) != 2 {
		t.Fatalf("expected 2 linked formulas, got %d: %v", len(linked), linked)
	}
}

func TestStrict_Undeclared(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/cellar/bar/1.0/lib/libbar.dylib", Kind: OtherKeg, Formula: "bar"},
				{Path: "/cellar/baz/2.0/lib/libbaz.dylib", Kind: OtherKeg, Formula: "baz"},
			}},
		},
	}
	sr := r.Strict([]string{"bar"})
	if len(sr.Undeclared) != 1 || sr.Undeclared[0] != "baz" {
		t.Errorf("expected undeclared=[baz], got %v", sr.Undeclared)
	}
	if len(sr.Unused) != 0 {
		t.Errorf("expected no unused, got %v", sr.Unused)
	}
}

func TestStrict_Unused(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/cellar/bar/1.0/lib/libbar.dylib", Kind: OtherKeg, Formula: "bar"},
			}},
		},
	}
	sr := r.Strict([]string{"bar", "qux"})
	if len(sr.Undeclared) != 0 {
		t.Errorf("expected no undeclared, got %v", sr.Undeclared)
	}
	if len(sr.Unused) != 1 || sr.Unused[0] != "qux" {
		t.Errorf("expected unused=[qux], got %v", sr.Unused)
	}
}

func TestStrict_Clean(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/cellar/bar/1.0/lib/libbar.dylib", Kind: OtherKeg, Formula: "bar"},
			}},
		},
	}
	sr := r.Strict([]string{"bar"})
	if len(sr.Undeclared) != 0 || len(sr.Unused) != 0 {
		t.Errorf("expected clean strict result, got undeclared=%v unused=%v", sr.Undeclared, sr.Unused)
	}
}

func TestFormatResult_Strict(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/cellar/bar/1.0/lib/libbar.dylib", Kind: OtherKeg, Formula: "bar"},
			}},
		},
	}
	sr := &StrictResult{
		Undeclared: []string{"bar"},
		Unused:     []string{"qux"},
	}
	out := FormatResult(r, FormatOpts{Strict: sr})
	if !strings.Contains(out, "Undeclared dependencies") {
		t.Errorf("expected 'Undeclared dependencies' section, got %q", out)
	}
	if !strings.Contains(out, "bar") {
		t.Errorf("expected 'bar' in undeclared, got %q", out)
	}
	if !strings.Contains(out, "Unused declared dependencies") {
		t.Errorf("expected 'Unused declared dependencies' section, got %q", out)
	}
	if !strings.Contains(out, "qux") {
		t.Errorf("expected 'qux' in unused, got %q", out)
	}
}

func TestFormatResult_StrictTestMode(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/usr/lib/libSystem.B.dylib", Kind: System},
			}},
		},
	}
	sr := &StrictResult{Undeclared: []string{"sneaky"}}
	out := FormatResult(r, FormatOpts{Test: true, Strict: sr})
	if !strings.Contains(out, "Undeclared dependencies") {
		t.Errorf("expected undeclared section in test+strict mode, got %q", out)
	}
}

// --quiet tests

func TestFormatResult_Quiet_NoBroken(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/tmp/cellar/foo/1.0/bin/foo", Deps: []Dep{
				{Path: "/usr/lib/libSystem.B.dylib", Kind: System},
			}},
		},
	}
	out := FormatResult(r, FormatOpts{Quiet: true})
	if out != "" {
		t.Errorf("expected empty output for quiet with no broken deps, got %q", out)
	}
}

func TestFormatResult_Quiet_Broken(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/tmp/cellar/foo/1.0/bin/foo", Deps: []Dep{
				{Path: "/usr/lib/libSystem.B.dylib", Kind: System},
				{Path: "/nonexistent/libfoo.dylib", Kind: Broken},
			}},
		},
	}
	out := FormatResult(r, FormatOpts{Quiet: true})
	if out != "/nonexistent/libfoo.dylib\n" {
		t.Errorf("expected just broken path, got %q", out)
	}
	if strings.Contains(out, "Broken") || strings.Contains(out, ":") {
		t.Errorf("quiet output should not contain headers, got %q", out)
	}
}

func TestFormatResult_Quiet_Test_NoBroken(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/tmp/cellar/foo/1.0/bin/foo", Deps: []Dep{
				{Path: "/usr/lib/libSystem.B.dylib", Kind: System},
			}},
		},
	}
	out := FormatResult(r, FormatOpts{Test: true, Quiet: true})
	if out != "" {
		t.Errorf("expected empty output for quiet+test with no broken deps, got %q", out)
	}
}

func TestFormatResult_Quiet_Test_Broken(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/tmp/cellar/foo/1.0/bin/foo", Deps: []Dep{
				{Path: "/nonexistent/libfoo.dylib", Kind: Broken},
				{Path: "/also/missing.dylib", Kind: Broken},
			}},
		},
	}
	out := FormatResult(r, FormatOpts{Test: true, Quiet: true})
	want := "/nonexistent/libfoo.dylib\n/also/missing.dylib\n"
	if out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

func TestFormatResult_Quiet_Strict(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/tmp/cellar/foo/1.0/bin/foo", Deps: []Dep{
				{Path: "/nonexistent/libfoo.dylib", Kind: Broken},
			}},
		},
	}
	strict := &StrictResult{
		Undeclared: []string{"bar"},
		Unused:     []string{"baz"},
	}
	out := FormatResult(r, FormatOpts{Quiet: true, Strict: strict})
	want := "/nonexistent/libfoo.dylib\nbar\nbaz\n"
	if out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

func TestFormatResult_Quiet_Dedup(t *testing.T) {
	t.Parallel()
	r := &Result{
		Name: "foo", Version: "1.0", KegPath: "/tmp/cellar/foo/1.0",
		Binaries: []BinaryResult{
			{Path: "/bin/a", Deps: []Dep{
				{Path: "/missing.dylib", Kind: Broken},
			}},
			{Path: "/bin/b", Deps: []Dep{
				{Path: "/missing.dylib", Kind: Broken},
			}},
		},
	}
	out := FormatResult(r, FormatOpts{Quiet: true})
	if out != "/missing.dylib\n" {
		t.Errorf("expected deduplicated output, got %q", out)
	}
}

// --reverse tests

func TestReverse_FindsDependents(t *testing.T) {
	t.Parallel()
	r := &ReverseResult{
		Name:    "libfoo",
		Version: "1.0",
		Entries: []ReverseEntry{
			{Formula: "bar", Binary: "/cellar/bar/1.0/bin/bar", Lib: "/cellar/libfoo/1.0/lib/libfoo.dylib"},
			{Formula: "baz", Binary: "/cellar/baz/2.0/bin/baz", Lib: "/cellar/libfoo/1.0/lib/libfoo.dylib"},
		},
	}
	out := FormatReverseResult(r, false)
	if !strings.Contains(out, "bar:") {
		t.Errorf("expected bar section in output, got:\n%s", out)
	}
	if !strings.Contains(out, "baz:") {
		t.Errorf("expected baz section in output, got:\n%s", out)
	}
	if !strings.Contains(out, "/cellar/bar/1.0/bin/bar => /cellar/libfoo/1.0/lib/libfoo.dylib") {
		t.Errorf("expected 'binary => lib' format in output, got:\n%s", out)
	}
}

func TestFormatReverseResult_Quiet(t *testing.T) {
	t.Parallel()
	r := &ReverseResult{
		Name:    "libfoo",
		Version: "1.0",
		Entries: []ReverseEntry{
			{Formula: "bar", Binary: "/cellar/bar/1.0/bin/bar", Lib: "/cellar/libfoo/1.0/lib/libfoo.dylib"},
			{Formula: "baz", Binary: "/cellar/baz/2.0/bin/baz", Lib: "/cellar/libfoo/1.0/lib/libfoo.dylib"},
			{Formula: "bar", Binary: "/cellar/bar/1.0/bin/bar2", Lib: "/cellar/libfoo/1.0/lib/libfoo.so"},
		},
	}
	out := FormatReverseResult(r, true)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (deduplicated), got %d: %q", len(lines), out)
	}
	if lines[0] != "bar" {
		t.Errorf("expected first line 'bar', got %q", lines[0])
	}
	if lines[1] != "baz" {
		t.Errorf("expected second line 'baz', got %q", lines[1])
	}
}

func TestFormatReverseResult_Empty(t *testing.T) {
	t.Parallel()
	r := &ReverseResult{Name: "libfoo", Version: "1.0"}
	out := FormatReverseResult(r, false)
	if !strings.Contains(out, "No formulas link against libfoo") {
		t.Errorf("expected empty message, got %q", out)
	}
}

func TestReverse_EmptyCellar(t *testing.T) {
	t.Parallel()
	r, err := Reverse("foo", "1.0", "/nonexistent/cellar/foo/1.0", "/nonexistent/cellar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Entries) != 0 {
		t.Errorf("expected no entries, got %d", len(r.Entries))
	}
}

func TestDepKindString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind DepKind
		want string
	}{
		{System, "system"},
		{Self, "self"},
		{OtherKeg, "other"},
		{Variable, "variable"},
		{Broken, "broken"},
		{DepKind(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("DepKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
		}
	}
}
