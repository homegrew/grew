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

	replacements := Replacements{
		"/opt/homebrew":       "/opt/homegrew",
		"@@HOMEBREW_PREFIX@@": "/opt/homegrew",
	}

	tests := []struct {
		name    string
		content string
		want    string
		mode    os.FileMode
	}{
		{
			name:    "test.el",
			content: "(setq path \"/opt/homebrew/share/emacs\")\n",
			want:    "(setq path \"/opt/homegrew/share/emacs\")\n",
		},
		{
			name:    "test.pc",
			content: "prefix=@@HOMEBREW_PREFIX@@\nlibdir=${prefix}/lib\n",
			want:    "prefix=/opt/homegrew\nlibdir=${prefix}/lib\n",
		},
		{
			name:    "test.sh",
			content: "#!/bin/sh\nexport PATH=/opt/homebrew/bin:$PATH\n",
			want:    "#!/bin/sh\nexport PATH=/opt/homegrew/bin:$PATH\n",
		},
		{
			name:    "test.txt",
			content: "/opt/homebrew is here but not whitelisted",
			want:    "/opt/homebrew is here but not whitelisted",
		},
		{
			name:    "readonly.conf",
			content: "path=/opt/homebrew/etc\n",
			want:    "path=/opt/homegrew/etc\n",
			mode:    0444,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.name)

			mode := tc.mode
			if mode == 0 {
				mode = 0644
			}

			if err := os.WriteFile(path, []byte(tc.content), mode); err != nil {
				t.Fatalf("failed to write %s: %v", tc.name, err)
			}

			if err := relocateTextFiles(dir, replacements); err != nil {
				t.Fatalf("relocateTextFiles failed: %v", err)
			}

			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read %s: %v", tc.name, err)
			}
			if string(content) != tc.want {
				t.Errorf("content = %q, want %q", string(content), tc.want)
			}

			if tc.mode != 0 {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("stat failed: %v", err)
				}
				if info.Mode().Perm() != tc.mode {
					t.Errorf("mode = %v, want %v", info.Mode().Perm(), tc.mode)
				}
			}
		})
	}
}

func TestOrderedKeysLongestFirst(t *testing.T) {
	t.Parallel()

	r := Replacements{
		PlaceholderCellar:                      "/opt/homegrew/Cellar",
		PlaceholderPrefix:                      "/opt/homegrew",
		PlaceholderCellar + "/gnutls/3.8.13_2": "/opt/homegrew/Cellar/gnutls/3.8.13",
		"/usr/local":                           "/opt/homegrew",
	}

	keys := r.OrderedKeys()
	for i := 1; i < len(keys); i++ {
		if len(keys[i-1]) < len(keys[i]) {
			t.Fatalf("OrderedKeys not longest-first: %q before %q", keys[i-1], keys[i])
		}
	}
	if keys[0] != PlaceholderCellar+"/gnutls/3.8.13_2" {
		t.Errorf("most specific key = %q, want %q", keys[0], PlaceholderCellar+"/gnutls/3.8.13_2")
	}
}

func TestApplyReplacementsMostSpecificWins(t *testing.T) {
	t.Parallel()

	// A self-referential dependency whose embedded version directory carries a
	// Homebrew revision suffix must be rewritten to the real keg path, not just
	// have its Cellar prefix swapped.
	r := Replacements{
		PlaceholderCellar:                      "/opt/homegrew/Cellar",
		PlaceholderCellar + "/gnutls/3.8.13_2": "/opt/homegrew/Cellar/gnutls/3.8.13",
	}

	got, changed := applyReplacements(PlaceholderCellar+"/gnutls/3.8.13_2/lib/libgnutls.30.dylib", r)
	if !changed {
		t.Fatal("expected a replacement to be applied")
	}
	want := "/opt/homegrew/Cellar/gnutls/3.8.13/lib/libgnutls.30.dylib"
	if got != want {
		t.Errorf("applyReplacements = %q, want %q", got, want)
	}
}

func TestEmbeddedKegVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		paths        []string
		formula      string
		installedVer string
		want         string
	}{
		{
			name:         "placeholder cellar with revision suffix",
			paths:        []string{"@@HOMEBREW_CELLAR@@/gnutls/3.8.13_2/lib/libgnutls.30.dylib"},
			formula:      "gnutls",
			installedVer: "3.8.13",
			want:         "3.8.13_2",
		},
		{
			name:         "real foreign cellar path",
			paths:        []string{"/opt/homebrew/Cellar/gnutls/3.8.13_2/lib/libgnutls.30.dylib"},
			formula:      "gnutls",
			installedVer: "3.8.13",
			want:         "3.8.13_2",
		},
		{
			name:         "matching version is not a mismatch",
			paths:        []string{"@@HOMEBREW_CELLAR@@/gnutls/3.8.13/lib/libgnutls.30.dylib"},
			formula:      "gnutls",
			installedVer: "3.8.13",
			want:         "",
		},
		{
			name:         "other formula is ignored",
			paths:        []string{"@@HOMEBREW_CELLAR@@/nettle/4.0/lib/libnettle.9.dylib"},
			formula:      "gnutls",
			installedVer: "3.8.13",
			want:         "",
		},
		{
			name:         "no cellar reference",
			paths:        []string{"@@HOMEBREW_PREFIX@@/opt/gnutls/lib/libgnutls.30.dylib", "/usr/lib/libSystem.B.dylib"},
			formula:      "gnutls",
			installedVer: "3.8.13",
			want:         "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := embeddedKegVersion(tc.paths, tc.formula, tc.installedVer)
			if got != tc.want {
				t.Errorf("embeddedKegVersion(%v) = %q, want %q", tc.paths, got, tc.want)
			}
		})
	}
}
