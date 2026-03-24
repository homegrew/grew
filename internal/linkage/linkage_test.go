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
	// A real path that exists on disk but is not in cellar — treated as system.
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
	out := FormatResult(r, false)
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
	out := FormatResult(r, true)
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
	out := FormatResult(r, true)
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
