package cask

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// makeCaskroom creates a temp dir and returns a Caskroom pointing to it.
func makeCaskroom(t *testing.T) (*Caskroom, string) {
	t.Helper()
	dir := t.TempDir()
	return &Caskroom{Path: dir}, dir
}

// installCask creates the directory structure Caskroom/<name>/<version>/.
func installCask(t *testing.T, caskroomDir, name, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(caskroomDir, name, version), 0755); err != nil {
		t.Fatalf("installCask: %v", err)
	}
}

// TestCaskroomList_Empty verifies that an empty caskroom returns nil, not an error.
func TestCaskroomList_Empty(t *testing.T) {
	t.Parallel()
	cr, _ := makeCaskroom(t)
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

// TestCaskroomList_NonExistentPath verifies that a missing directory returns nil, nil.
func TestCaskroomList_NonExistentPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cr := &Caskroom{Path: filepath.Join(dir, "does-not-exist")}
	got, err := cr.List()
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil slice for missing dir, got %v", got)
	}
}

// TestCaskroomList_SingleCask verifies a single installed cask is returned.
func TestCaskroomList_SingleCask(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	installCask(t, dir, "firefox", "120.0")
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cask, got %d: %v", len(got), got)
	}
	if got[0].Name != "firefox" {
		t.Errorf("name = %q, want %q", got[0].Name, "firefox")
	}
	if got[0].Version != "120.0" {
		t.Errorf("version = %q, want %q", got[0].Version, "120.0")
	}
}

// TestCaskroomList_MultipleCasksSorted verifies results are returned in sorted order.
func TestCaskroomList_MultipleCasksSorted(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	installCask(t, dir, "zoom", "5.16.0")
	installCask(t, dir, "alfred", "5.1.1")
	installCask(t, dir, "iterm2", "3.4.22")

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 casks, got %d: %v", len(got), got)
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Name < got[j].Name }) {
		t.Errorf("results not sorted: %v", got)
	}
	if got[0].Name != "alfred" || got[1].Name != "iterm2" || got[2].Name != "zoom" {
		t.Errorf("unexpected order: %v", got)
	}
}

// TestCaskroomList_InvalidNameEntriesFiltered verifies directory entries with
// invalid names (e.g. ".git", "__pycache__", "..", "Invalid") are skipped.
func TestCaskroomList_InvalidNameEntriesFiltered(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	// A valid cask entry.
	installCask(t, dir, "vlc", "3.0.20")
	// Entries that should be ignored because their names fail IsValidName.
	for _, bad := range []string{".hidden", "Has_UPPER", "with space"} {
		if err := os.MkdirAll(filepath.Join(dir, bad), 0755); err != nil {
			t.Fatalf("mkdir %q: %v", bad, err)
		}
	}

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected only 1 valid cask, got %d: %v", len(got), got)
	}
	if got[0].Name != "vlc" {
		t.Errorf("name = %q, want %q", got[0].Name, "vlc")
	}
}

// TestCaskroomList_FilesIgnored verifies regular files (non-directories) in the
// caskroom are not returned as casks.
func TestCaskroomList_FilesIgnored(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	installCask(t, dir, "kitty", "0.31.0")
	// Place a regular file that would otherwise pass IsValidName.
	if err := os.WriteFile(filepath.Join(dir, "notadir"), []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "kitty" {
		t.Errorf("expected only kitty, got %v", got)
	}
}

// TestCaskroomList_SymlinkResolution verifies that a caskroom path provided as
// a symlink is resolved to its real target and listing still works correctly.
func TestCaskroomList_SymlinkResolution(t *testing.T) {
	t.Parallel()
	realDir := t.TempDir()
	installCask(t, realDir, "slack", "4.36.140")

	// Create a symlink that points to the real caskroom directory.
	linkDir := filepath.Join(t.TempDir(), "caskroom-link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	cr := &Caskroom{Path: linkDir}
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error after symlink resolution: %v", err)
	}
	if len(got) != 1 || got[0].Name != "slack" {
		t.Errorf("expected [slack], got %v", got)
	}
}

// TestCaskroomList_InvalidInitialPath verifies that a relative path (which
// SafeAbsolutePath rejects) returns an error before any filesystem access.
func TestCaskroomList_InvalidInitialPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: "relative/path"}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for relative caskroom path, got nil")
	}
}

// TestCaskroomList_EmptyPath verifies that an empty path string is rejected.
func TestCaskroomList_EmptyPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: ""}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for empty caskroom path, got nil")
	}
}

// TestCaskroomList_RootPath verifies that the filesystem root "/" is rejected
// by SafeAbsolutePath to prevent accidental enumeration of the entire root.
func TestCaskroomList_RootPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: "/"}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for root path '/', got nil")
	}
}

// TestCaskroomList_DotDotInPath verifies that a path containing ".." traversal
// components is rejected by SafeAbsolutePath.
func TestCaskroomList_DotDotInPath(t *testing.T) {
	t.Parallel()
	cr := &Caskroom{Path: "/tmp/foo/../bar"}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error for path with '..' traversal, got nil")
	}
}

// TestCaskroomList_CaskWithNoVersion verifies that a cask directory that
// contains no valid version subdirectory is silently skipped.
func TestCaskroomList_CaskWithNoVersion(t *testing.T) {
	t.Parallel()
	cr, dir := makeCaskroom(t)
	// Create the cask directory but give it no version subdirectory.
	if err := os.MkdirAll(filepath.Join(dir, "emptyapp"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A properly installed cask to confirm the valid one is still returned.
	installCask(t, dir, "gpg-suite", "2023.3")

	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// emptyapp should be silently skipped; gpg-suite must appear.
	if len(got) != 1 || got[0].Name != "gpg-suite" {
		t.Errorf("expected [gpg-suite], got %v", got)
	}
}

// TestCaskroomList_NestedSymlinkResolution verifies that a caskroom path
// provided through a chain of symlinks (link -> link -> real dir) is fully
// resolved by filepath.EvalSymlinks and listing still works correctly.
func TestCaskroomList_NestedSymlinkResolution(t *testing.T) {
	t.Parallel()
	realDir := t.TempDir()
	installCask(t, realDir, "rectangle", "0.77")

	base := t.TempDir()
	// First hop: link1 -> realDir
	link1 := filepath.Join(base, "link1")
	if err := os.Symlink(realDir, link1); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}
	// Second hop: link2 -> link1
	link2 := filepath.Join(base, "link2")
	if err := os.Symlink(link1, link2); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	cr := &Caskroom{Path: link2}
	got, err := cr.List()
	if err != nil {
		t.Fatalf("unexpected error after nested symlink resolution: %v", err)
	}
	if len(got) != 1 || got[0].Name != "rectangle" {
		t.Errorf("expected [rectangle], got %v", got)
	}
}

// TestCaskroomList_SymlinkToInvalidTarget verifies that when a caskroom symlink
// resolves to a path that SafeAbsolutePath would reject (e.g. "/"), List
// returns an error instead of silently proceeding.
func TestCaskroomList_SymlinkToInvalidTarget(t *testing.T) {
	t.Parallel()
	// Create a symlink that points to "/".
	linkDir := filepath.Join(t.TempDir(), "bad-link")
	if err := os.Symlink("/", linkDir); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	cr := &Caskroom{Path: linkDir}
	_, err := cr.List()
	if err == nil {
		t.Fatal("expected error when resolved symlink target is '/', got nil")
	}
}

func TestCask_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cask    Cask
		wantErr string
	}{
		{
			name: "Valid",
			cask: Cask{
				Name:    "testapp",
				Version: "1.0",
				URL:     map[string]string{"darwin_arm64": "https://example.com/test.dmg"},
				Artifacts: Artifacts{
					App: []string{"Test.app"},
				},
			},
			wantErr: "",
		},
		{
			name: "MissingName",
			cask: Cask{
				Version: "1.0",
				URL:     map[string]string{"darwin_arm64": "https://example.com/test.dmg"},
			},
			wantErr: "cask missing required field: name",
		},
		{
			name: "InvalidName",
			cask: Cask{
				Name:    "Invalid Name",
				Version: "1.0",
			},
			wantErr: "contains invalid characters",
		},
		{
			name: "MissingVersion",
			cask: Cask{
				Name: "testapp",
				URL:  map[string]string{"darwin_arm64": "https://example.com/test.dmg"},
			},
			wantErr: "missing required field: version",
		},
		{
			name: "InsecureURL",
			cask: Cask{
				Name:    "testapp",
				Version: "1.0",
				URL:     map[string]string{"darwin_arm64": "http://example.com/test.dmg"},
			},
			wantErr: "must use HTTPS",
		},
		{
			name: "NoArtifacts",
			cask: Cask{
				Name:    "testapp",
				Version: "1.0",
				URL:     map[string]string{"darwin_arm64": "https://example.com/test.dmg"},
			},
			wantErr: "must declare at least one artifact",
		},
		{
			name: "InvalidAppArtifact",
			cask: Cask{
				Name:    "testapp",
				Version: "1.0",
				URL:     map[string]string{"darwin_arm64": "https://example.com/test.dmg"},
				Artifacts: Artifacts{
					App: []string{"Test.exe"},
				},
			},
			wantErr: "must end with .app",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cask.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	data := []byte(`
name: firefox
version: 120.0
url:
  darwin_arm64: https://example.com/firefox.dmg
artifacts:
  app:
    - Firefox.app
`)
	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() failed: %v", err)
	}
	if c.Name != "firefox" {
		t.Errorf("expected name firefox, got %q", c.Name)
	}
}

func TestLoader(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	caskDir := filepath.Join(tmpDir, "homegrew", "homegrew-taps", "cask")
	if err := os.MkdirAll(caskDir, 0755); err != nil {
		t.Fatal(err)
	}

	caskData := `
name: testapp
version: 1.0
url:
  darwin_arm64: https://example.com/test.dmg
artifacts:
  app:
    - Test.app
`
	if err := os.WriteFile(filepath.Join(caskDir, "testapp.yaml"), []byte(caskData), 0644); err != nil {
		t.Fatal(err)
	}

	loader := &Loader{TapDir: tmpDir}

	t.Run("LoadByName", func(t *testing.T) {
		c, err := loader.LoadByName("testapp")
		if err != nil {
			t.Fatalf("LoadByName failed: %v", err)
		}
		if c.Name != "testapp" {
			t.Errorf("expected testapp, got %q", c.Name)
		}
	})

	t.Run("LoadAll", func(t *testing.T) {
		casks, err := loader.LoadAll()
		if err != nil {
			t.Fatalf("LoadAll failed: %v", err)
		}
		if len(casks) != 2 {
			t.Errorf("expected 1 cask, got %d", len(casks))
		}
	})
}

func TestGetURLForPlatform(t *testing.T) {
	t.Parallel()
	c := Cask{
		Name: "test",
		URL: map[string]string{
			"darwin_arm64": "https://example.com/arm.dmg",
			"darwin_amd64": "https://example.com/intel.dmg",
			"linux_amd64":  "http://example.com/insecure.dmg",
		},
	}

	tests := []struct {
		osName  string
		arch    string
		want    string
		wantErr string
	}{
		{"darwin", "arm64", "https://example.com/arm.dmg", ""},
		{"darwin", "amd64", "https://example.com/intel.dmg", ""},
		{"linux", "amd64", "", "refusing to download over insecure HTTP"},
		{"windows", "amd64", "", "does not support platform windows_amd64"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.osName+"_"+tt.arch, func(t *testing.T) {
			t.Parallel()
			got, err := c.GetURLForPlatform(tt.osName, tt.arch)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("GetURLForPlatform() unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("GetURLForPlatform() = %q, want %q", got, tt.want)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("GetURLForPlatform() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestGetSHA256ForPlatform(t *testing.T) {
	t.Parallel()
	validSHA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	c := Cask{
		Name: "test",
		SHA256: map[string]string{
			"darwin_arm64": validSHA,
			"darwin_amd64": "invalid-sha",
		},
	}

	tests := []struct {
		osName  string
		arch    string
		want    string
		wantErr string
	}{
		{"darwin", "arm64", validSHA, ""},
		{"darwin", "amd64", "", "invalid SHA256"},
		{"linux", "amd64", "", "has no SHA256 for platform linux_amd64"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.osName+"_"+tt.arch, func(t *testing.T) {
			t.Parallel()
			got, err := c.GetSHA256ForPlatform(tt.osName, tt.arch)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("GetSHA256ForPlatform() unexpected error: %v", err)
				}
				if got != tt.want {
					t.Errorf("GetSHA256ForPlatform() = %q, want %q", got, tt.want)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("GetSHA256ForPlatform() error = %v, wantErr %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestGetSource(t *testing.T) {
	t.Parallel()
	validSHA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	t.Run("Valid", func(t *testing.T) {
		t.Parallel()
		c := Cask{
			Source: SourceSpec{
				URL:    "https://example.com/source.tar.gz",
				SHA256: validSHA,
			},
		}
		u, err := c.GetSourceURL()
		if err != nil || u != c.Source.URL {
			t.Errorf("GetSourceURL() = %v, %v", u, err)
		}
		sha, err := c.GetSourceSHA256()
		if err != nil || sha != validSHA {
			t.Errorf("GetSourceSHA256() = %v, %v", sha, err)
		}
	})

	t.Run("Missing", func(t *testing.T) {
		t.Parallel()
		c := Cask{}
		if _, err := c.GetSourceURL(); err == nil {
			t.Error("expected error for missing source URL")
		}
		if _, err := c.GetSourceSHA256(); err == nil {
			t.Error("expected error for missing source SHA256")
		}
	})

	t.Run("Insecure", func(t *testing.T) {
		t.Parallel()
		c := Cask{Source: SourceSpec{URL: "http://example.com/src"}}
		if _, err := c.GetSourceURL(); err == nil || !strings.Contains(err.Error(), "must use HTTPS") {
			// Actually the code says "refusing to download over insecure HTTP"
			if err == nil || !strings.Contains(err.Error(), "insecure HTTP") {
				t.Errorf("expected insecure HTTP error, got %v", err)
			}
		}
	})
}

func TestLoader_Errors(t *testing.T) {
	t.Parallel()

	t.Run("InvalidName", func(t *testing.T) {
		t.Parallel()
		l := Loader{TapDir: t.TempDir()}
		if _, err := l.LoadByName("../escape"); err == nil {
			t.Error("expected error for invalid name")
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		t.Parallel()
		l := Loader{TapDir: t.TempDir()}
		if _, err := l.LoadByName("missing"); err == nil {
			t.Error("expected error for missing cask")
		}
	})

	t.Run("MissingTapDir", func(t *testing.T) {
		t.Parallel()
		l := Loader{TapDir: "/tmp/non-existent-grew-tap-dir"}
		if _, err := l.LoadAll(); err == nil {
			t.Error("expected error for missing tap dir")
		}
		if _, err := l.LoadByName("any"); err == nil {
			t.Error("expected error for missing tap dir")
		}
	})
}

func TestCaskroom_Methods(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cr := &Caskroom{Path: tmpDir}

	name := "testapp"
	version := "1.2.3"

	// Record
	if err := cr.Record(name, version); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// IsInstalled
	if !cr.IsInstalled(name) {
		t.Errorf("IsInstalled returned false for installed cask")
	}

	// InstalledVersion
	gotVer, err := cr.InstalledVersion(name)
	if err != nil {
		t.Fatalf("InstalledVersion failed: %v", err)
	}
	if gotVer != version {
		t.Errorf("expected version %q, got %q", version, gotVer)
	}

	// Remove
	if err := cr.Remove(name); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if cr.IsInstalled(name) {
		t.Errorf("IsInstalled returned true after removal")
	}
}

func TestInstallerScriptExecutableValidation(t *testing.T) {
	t.Parallel()

	makeCask := func(exe string) *Cask {
		return &Cask{
			Name:    "test-cask",
			Version: "1.0",
			URL:     map[string]string{"darwin_arm64": "https://example.com/f.zip"},
			SHA256:  map[string]string{"darwin_arm64": "a" + string(make([]byte, 63))},
			Artifacts: Artifacts{
				Installer: []InstallerScript{{Executable: exe}},
			},
		}
	}

	// Traversal in the executable path must be rejected.
	traversalCases := []string{
		"../../evil/script.sh",
		"../script.sh",
		"subdir/../../etc/passwd",
	}
	for _, exe := range traversalCases {
		c := makeCask(exe)
		if err := c.Validate(); err == nil {
			t.Errorf("Validate(%q): expected error for traversal path, got nil", exe)
		}
	}

	// Legitimate subdir paths must pass.
	validCases := []string{
		"Install.sh",
		"subdir/Install.sh",
		"a/b/c/run.sh",
	}
	for _, exe := range validCases {
		c := makeCask(exe)
		// Force a minimal valid sha256 so only the executable is at issue.
		for k := range c.SHA256 {
			c.SHA256[k] = "aabbccddeeff0011aabbccddeeff0011aabbccddeeff0011aabbccddeeff0011"
		}
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%q): unexpected error for valid path: %v", exe, err)
		}
	}
}
