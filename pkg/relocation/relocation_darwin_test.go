package relocation

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRelocateRevisionedSelfRef reproduces the gnutls failure: a bottle whose
// inter-binary dependency points at @@HOMEBREW_CELLAR@@/<name>/<ver>_<rev>
// while grew installs the keg under plain <ver>.
func TestRelocateRevisionedSelfRef(t *testing.T) {
	for _, tool := range []string{"clang", "install_name_tool", "codesign", "otool"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available: %v", tool, err)
		}
	}

	prefix := t.TempDir()
	keg := filepath.Join(prefix, "Cellar", "gnutls", "3.8.13")
	libDir := filepath.Join(keg, "lib")
	binDir := filepath.Join(keg, "bin")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	libSrc := filepath.Join(t.TempDir(), "lib.c")
	os.WriteFile(libSrc, []byte("int gnutls_init(void){return 42;}\n"), 0o644)
	dylib := filepath.Join(libDir, "libgnutls.30.dylib")
	// Build with the placeholder install name, as Homebrew bottles do.
	run(t, "clang", "-Wl,-headerpad_max_install_names", "-dynamiclib", "-o", dylib,
		"-install_name", "@@HOMEBREW_PREFIX@@/opt/gnutls/lib/libgnutls.30.dylib", libSrc)

	binSrc := filepath.Join(t.TempDir(), "main.c")
	os.WriteFile(binSrc, []byte("extern int gnutls_init(void); int main(void){return gnutls_init();}\n"), 0o644)
	bin := filepath.Join(binDir, "gnutls-cli")
	run(t, "clang", "-Wl,-headerpad_max_install_names", "-o", bin, binSrc, dylib)
	// Rewrite the dep to the revisioned placeholder Cellar path, mimicking the bottle.
	run(t, "install_name_tool", "-change",
		"@@HOMEBREW_PREFIX@@/opt/gnutls/lib/libgnutls.30.dylib",
		"@@HOMEBREW_CELLAR@@/gnutls/3.8.13_2/lib/libgnutls.30.dylib", bin)
	run(t, "codesign", "--force", "--sign", "-", bin)

	// The installer creates opt/<name> -> keg after relocation; mirror that so
	// the dylib's @@HOMEBREW_PREFIX@@/opt/... install name resolves.
	if err := os.MkdirAll(filepath.Join(prefix, "opt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(keg, filepath.Join(prefix, "opt", "gnutls")); err != nil {
		t.Fatal(err)
	}

	if err := RelocateKeg(keg, prefix); err != nil {
		t.Fatalf("RelocateKeg: %v", err)
	}
	if issues := VerifyKeg(keg, prefix); len(issues) != 0 {
		t.Fatalf("VerifyKeg found %d issue(s): %v", len(issues), issues)
	}
}

// TestVerifyIgnoresNonMachOData reproduces the qemu failure: foreign-arch
// firmware/ELF blobs shipped under share/ have executable-like magic bytes but
// are not Mach-O objects. otool reports "is not an object file" on stdout with a
// zero exit status; verification must skip such files, not treat the message as
// a missing dependency.
func TestVerifyIgnoresNonMachOData(t *testing.T) {
	if _, err := exec.LookPath("otool"); err != nil {
		t.Skipf("otool not available: %v", err)
	}

	prefix := t.TempDir()
	keg := filepath.Join(prefix, "Cellar", "qemu", "11.0.1")
	shareDir := filepath.Join(keg, "share", "qemu")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A foreign (PPC) ELF firmware image — isBinary matches the ELF magic, but
	// otool cannot inspect it.
	blob := filepath.Join(shareDir, "openbios-ppc")
	if err := os.WriteFile(blob, []byte("\x7fELF\x02\x02\x01\x00ppc firmware payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isBinary(blob) {
		t.Fatal("test fixture should be detected as a binary by magic bytes")
	}

	if issues := VerifyKeg(keg, prefix); len(issues) != 0 {
		t.Fatalf("VerifyKeg found %d issue(s) on non-Mach-O data: %v", len(issues), issues)
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
