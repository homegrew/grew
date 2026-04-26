package relocation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveOldPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{
			name:  "empty",
			paths: nil,
			want:  "",
		},
		{
			name:  "no markers",
			paths: []string{"/usr/lib/libfoo.dylib", "/usr/local/bin/tool"},
			want:  "",
		},
		{
			name:  "Cellar marker",
			paths: []string{"/home/runner/Cellar/pkg/1.0/lib/libfoo.dylib"},
			want:  "/home/runner",
		},
		{
			name:  "opt marker",
			paths: []string{"/tmp/build/opt/grew/lib/libbar.so"},
			want:  "/tmp/build",
		},
		{
			name:  "lib marker should not match",
			paths: []string{"/usr/lib/libfoo.dylib"},
			want:  "",
		},
		{
			name:  "system lib path should not yield a prefix",
			paths: []string{"/usr/lib/x86_64-linux-gnu/libfoo.so"},
			want:  "",
		},
		{
			name:  "first matching path wins",
			paths: []string{"/ci/Cellar/pkg/1.0/bin/tool", "/other/opt/grew/bin/tool"},
			want:  "/ci",
		},
		{
			name:  "relative path is rejected",
			paths: []string{"relative/Cellar/pkg/1.0/lib/libfoo.dylib"},
			want:  "",
		},
		{
			name:  "marker at start of path (idx==0) is skipped",
			paths: []string{"/Cellar/pkg/1.0/lib/libfoo.dylib"},
			want:  "",
		},
		{
			name:  "prefix under /opt with nested opt dir",
			paths: []string{"/opt/grew/opt/bar/lib/libbar.dylib"},
			want:  "/opt/grew",
		},
		{
			name:  "prefix under /opt with Cellar path",
			paths: []string{"/opt/grew/Cellar/foo/1.0/lib/libfoo.dylib"},
			want:  "/opt/grew",
		},
		{
			name:  "prefix under /opt only single opt segment",
			paths: []string{"/opt/foo/lib/libfoo.dylib"},
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deriveOldPrefix(tc.paths)
			if got != tc.want {
				t.Errorf("deriveOldPrefix(%v) = %q, want %q", tc.paths, got, tc.want)
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		magic []byte
		want  bool
	}{
		{"macho64-le", []byte{0xCF, 0xFA, 0xED, 0xFE}, true},
		{"macho64-be", []byte{0xFE, 0xED, 0xFA, 0xCF}, true},
		{"macho32-le", []byte{0xCE, 0xFA, 0xED, 0xFE}, true},
		{"macho32-be", []byte{0xFE, 0xED, 0xFA, 0xCE}, true},
		{"fat-magic", []byte{0xCA, 0xFE, 0xBA, 0xBE}, true},
		{"fat-cigam", []byte{0xBE, 0xBA, 0xFE, 0xCA}, true},
		{"elf", []byte{0x7F, 'E', 'L', 'F'}, true},
		{"text", []byte{0x23, 0x21, 0x2F, 0x62}, false}, // "#!/b"
		{"empty", []byte{}, false},
	}

	dir := t.TempDir()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, tc.name)
			// Pad to at least 8 bytes so the updated magic read succeeds.
			// Specifically for fat-magic, we add a mock narchs count of 2.
			data := make([]byte, 8)
			copy(data, tc.magic)
			if tc.name == "fat-magic" {
				data[4], data[5], data[6], data[7] = 0x00, 0x00, 0x00, 0x02
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			got := isBinary(path)
			if got != tc.want {
				t.Errorf("isBinary(%q magic=%X) = %v, want %v", tc.name, tc.magic, got, tc.want)
			}
		})
	}
}

func TestIsBinary_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if isBinary(path) {
		t.Error("empty file should not be detected as binary")
	}
}

func TestIsBinary_MissingFile(t *testing.T) {
	t.Parallel()
	if isBinary("/nonexistent/path/to/file") {
		t.Error("missing file should not be detected as binary")
	}
}

func TestRelocateTextFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	replacements := Replacements{
		"/opt/homebrew":       "/opt/homegrew",
		"@@HOMEBREW_PREFIX@@": "/opt/homegrew",
	}

	files := map[string]string{
		"test.el":      "(setq path \"/opt/homebrew/share/emacs\")\n",
		"test.pc":      "prefix=@@HOMEBREW_PREFIX@@\nlibdir=${prefix}/lib\n",
		"test.sh":      "#!/bin/sh\nexport PATH=/opt/homebrew/bin:$PATH\n",
		"test.txt":     "/opt/homebrew is here but not whitelisted",
		"readonly.conf": "path=/opt/homebrew/etc\n",
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
		if name == "readonly.conf" {
			// Make it read-only
			if err := os.Chmod(path, 0444); err != nil {
				t.Fatalf("failed to chmod %s: %v", name, err)
			}
		}
	}

	if err := relocateTextFiles(dir, replacements); err != nil {
		t.Fatalf("relocateTextFiles failed: %v", err)
	}

	checkFile := func(name, want string) {
		t.Helper()
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		if string(content) != want {
			t.Errorf("%s content = %q, want %q", name, string(content), want)
		}
		// Verify readonly.conf is back to read-only (or at least was modified)
		if name == "readonly.conf" {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat failed: %v", err)
			}
			if info.Mode()&0200 != 0 {
				t.Errorf("%s should still be read-only (mode: %v)", name, info.Mode())
			}
		}
	}

	checkFile("test.el", "(setq path \"/opt/homegrew/share/emacs\")\n")
	checkFile("test.pc", "prefix=/opt/homegrew\nlibdir=${prefix}/lib\n")
	checkFile("test.sh", "#!/bin/sh\nexport PATH=/opt/homegrew/bin:$PATH\n")
	checkFile("test.txt", "/opt/homebrew is here but not whitelisted")
	checkFile("readonly.conf", "path=/opt/homegrew/etc\n")
}
