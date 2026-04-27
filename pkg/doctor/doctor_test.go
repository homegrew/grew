package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/sandbox"
	"github.com/homegrew/grew/internal/snapshot"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/linker"
	grewrt "github.com/homegrew/grew/internal/runtime"
)

func TestBaseChecks(t *testing.T) {
	t.Parallel()
	checks := BaseChecks()
	if len(checks) == 0 {
		t.Error("BaseChecks() returned no checks")
	}

	for _, c := range checks {
		if c.Name == "" {
			t.Errorf("check %+v has no name", c)
		}
		if c.Desc == "" {
			t.Errorf("check %s has no description", c.Name)
		}
		if c.Run == nil {
			t.Errorf("check %s has no Run function", c.Name)
		}
	}
}

func assertDoctorWarnings(t *testing.T, ctx *Context, checkFn func(*Context), wantWarn string) {
	t.Helper()
	var warnings []string
	ctx.Warn = func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	checkFn(ctx)

	if wantWarn == "" {
		if len(warnings) > 0 {
			t.Errorf("expected no warnings, got: %v", warnings)
		}
	} else {
		found := false
		for _, w := range warnings {
			if strings.Contains(w, wantWarn) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected warning containing %q, got: %v", wantWarn, warnings)
		}
	}
}

func TestValidHexHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		hash        string
		expectedLen int
		want        string
	}{
		{"valid 64", strings.Repeat("a", 64), 64, ""},
		{"valid 128", strings.Repeat("F", 128), 128, ""},
		{"no_check", "no_check", 64, ""},
		{"empty", "", 64, ""},
		{"wrong length", "abc", 64, "has wrong length (3, expected 64)"},
		{"invalid char", strings.Repeat("z", 64), 64, "contains non-hex character \"z\""},
		{"spaces padded", "  " + strings.Repeat("1", 64) + "  ", 64, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidHexHash(tt.hash, tt.expectedLen); got != tt.want {
				t.Errorf("ValidHexHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckDirectoryPermissions(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	t.Run("safe permissions", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join(tmpDir, "safe_root")
		os.MkdirAll(root, 0755)
		ctx := &Context{Paths: config.Paths{Root: root}}
		assertDoctorWarnings(t, ctx, CheckDirectoryPermissions, "")
	})

	t.Run("world-writable permissions", func(t *testing.T) {
		t.Parallel()
		root := filepath.Join(tmpDir, "unsafe_root")
		os.MkdirAll(root, 0755)
		if err := os.Chmod(root, 0777); err != nil {
			t.Fatal(err)
		}
		ctx := &Context{Paths: config.Paths{Root: root}}
		assertDoctorWarnings(t, ctx, CheckDirectoryPermissions, "is world-writable")
	})
}

func TestCheckFormulaHTTPS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		formulas []*formula.Formula
		wantWarn string
	}{
		{
			name: "valid https",
			formulas: []*formula.Formula{
				{
					Name: "foo",
					URL:  map[string]string{"darwin_arm64": "https://example.com/foo.tar.gz"},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid http",
			formulas: []*formula.Formula{
				{
					Name: "foo",
					URL:  map[string]string{"darwin_arm64": "http://example.com/foo.tar.gz"},
				},
			},
			wantWarn: "formula foo: URL for darwin_arm64 uses insecure HTTP: http://example.com/foo.tar.gz",
		},
		{
			name: "git protocol",
			formulas: []*formula.Formula{
				{
					Name: "foo",
					URL:  map[string]string{"darwin_arm64": "git://example.com/foo.git"},
				},
			},
			wantWarn: "formula foo: URL for darwin_arm64 uses insecure HTTP: git://example.com/foo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &Context{Formulas: tt.formulas}
			assertDoctorWarnings(t, ctx, CheckFormulaHTTPS, tt.wantWarn)
		})
	}
}

func TestCheckFormulaSHA256(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		formulas []*formula.Formula
		wantWarn string
	}{
		{
			name: "valid sha256",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("a", 64)},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid sha256 length",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": "abc"},
				},
			},
			wantWarn: "formula foo: SHA256 for darwin_arm64 has wrong length (3, expected 64)",
		},
		{
			name: "invalid sha256 characters",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("z", 64)},
				},
			},
			wantWarn: "formula foo: SHA256 for darwin_arm64 contains non-hex character \"z\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &Context{Formulas: tt.formulas}
			assertDoctorWarnings(t, ctx, CheckFormulaSHA256, tt.wantWarn)
		})
	}
}

func TestCheckCoreTap(t *testing.T) {
	t.Parallel()
	t.Run("with formulas", func(t *testing.T) {
		t.Parallel()
		ctx := &Context{Formulas: []*formula.Formula{{Name: "foo"}}}
		assertDoctorWarnings(t, ctx, CheckCoreTap, "")
	})

	t.Run("without formulas", func(t *testing.T) {
		t.Parallel()
		ctx := &Context{Formulas: []*formula.Formula{}}
		assertDoctorWarnings(t, ctx, CheckCoreTap, "no formulas found in any tap")
	})
}

func TestCheckPrefixIsolation(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("could not get home dir")
	}

	t.Run("inside home", func(t *testing.T) {
		t.Parallel()
		ctx := &Context{Paths: config.Paths{Root: filepath.Join(home, ".grew")}}
		want := fmt.Sprintf("grew prefix %s is under $HOME", ctx.Paths.Root)
		assertDoctorWarnings(t, ctx, CheckPrefixIsolation, want)
	})

	t.Run("outside home", func(t *testing.T) {
		t.Parallel()
		ctx := &Context{Paths: config.Paths{Root: grewrt.SystemPrefix()}}
		assertDoctorWarnings(t, ctx, CheckPrefixIsolation, "")
	})
}

func TestCheckDirectories(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// All directories exist
	t.Run("all present", func(t *testing.T) {
		t.Parallel()
		paths := config.Paths{
			Root:    filepath.Join(tmpDir, "root"),
			Cellar:  filepath.Join(tmpDir, "cellar"),
			Opt:     filepath.Join(tmpDir, "opt"),
			Bin:     filepath.Join(tmpDir, "bin"),
			Lib:     filepath.Join(tmpDir, "lib"),
			Include: filepath.Join(tmpDir, "include"),
			Taps:    filepath.Join(tmpDir, "taps"),
			CoreTap: filepath.Join(tmpDir, "coretap"),
			Tmp:     filepath.Join(tmpDir, "tmp"),
		}
		os.MkdirAll(paths.Root, 0755)
		os.MkdirAll(paths.Cellar, 0755)
		os.MkdirAll(paths.Opt, 0755)
		os.MkdirAll(paths.Bin, 0755)
		os.MkdirAll(paths.Lib, 0755)
		os.MkdirAll(paths.Include, 0755)
		os.MkdirAll(paths.Taps, 0755)
		os.MkdirAll(paths.CoreTap, 0755)
		os.MkdirAll(paths.Tmp, 0755)

		ctx := &Context{Paths: paths}
		assertDoctorWarnings(t, ctx, CheckDirectories, "")
	})

	// Missing one directory
	t.Run("missing Cellar", func(t *testing.T) {
		t.Parallel()
		paths := config.Paths{
			Root:    filepath.Join(tmpDir, "root2"),
			Cellar:  filepath.Join(tmpDir, "root2", "cellar_missing"),
		}
		os.MkdirAll(paths.Root, 0755)
		ctx := &Context{Paths: paths}
		assertDoctorWarnings(t, ctx, CheckDirectories, "Cellar directory missing")
	})
}

func TestCheckStaleTmp(t *testing.T) {
	t.Parallel()

	t.Run("empty tmp", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		ctx := &Context{Paths: config.Paths{Tmp: tmpDir}}
		assertDoctorWarnings(t, ctx, CheckStaleTmp, "")
	})

	t.Run("stale files present", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "stale.txt"), []byte("foo"), 0644)
		ctx := &Context{Paths: config.Paths{Tmp: tmpDir}}
		assertDoctorWarnings(t, ctx, CheckStaleTmp, "1 leftover file(s) in tmp directory")
	})
}

func TestCheckPath(t *testing.T) {
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")

	t.Run("in path", func(t *testing.T) {
		os.Setenv("PATH", binDir+string(filepath.ListSeparator)+origPath)
		ctx := &Context{Paths: config.Paths{Bin: binDir}}
		assertDoctorWarnings(t, ctx, CheckPath, "")
	})

	t.Run("not in path", func(t *testing.T) {
		os.Setenv("PATH", origPath) // assuming tmpDir isn't in origPath
		ctx := &Context{Paths: config.Paths{Bin: binDir}}
		assertDoctorWarnings(t, ctx, CheckPath, "is not in your PATH")
	})
}

func TestCheckBrokenSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("valid symlink", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		os.MkdirAll(binDir, 0755)
		target := filepath.Join(tmpDir, "target")
		os.WriteFile(target, []byte("test"), 0644)
		os.Symlink(target, filepath.Join(binDir, "valid_link"))
		
		ctx := &Context{Paths: config.Paths{Bin: binDir}}
		assertDoctorWarnings(t, ctx, CheckBrokenSymlinks, "")
	})

	t.Run("broken symlink", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		os.MkdirAll(binDir, 0755)
		os.Symlink(filepath.Join(tmpDir, "missing"), filepath.Join(binDir, "broken_link"))
		
		ctx := &Context{Paths: config.Paths{Bin: binDir}}
		assertDoctorWarnings(t, ctx, CheckBrokenSymlinks, "broken symlink")
	})
}

func TestCheckBrokenOptSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("valid opt symlink", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		optDir := filepath.Join(tmpDir, "opt")
		os.MkdirAll(optDir, 0755)
		target := filepath.Join(tmpDir, "target_opt")
		os.WriteFile(target, []byte("test"), 0644)
		os.Symlink(target, filepath.Join(optDir, "valid_link"))
		
		ctx := &Context{Paths: config.Paths{Opt: optDir}}
		assertDoctorWarnings(t, ctx, CheckBrokenOptSymlinks, "")
	})

	t.Run("broken opt symlink", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		optDir := filepath.Join(tmpDir, "opt")
		os.MkdirAll(optDir, 0755)
		os.Symlink(filepath.Join(tmpDir, "missing_opt"), filepath.Join(optDir, "broken_link"))
		
		ctx := &Context{Paths: config.Paths{Opt: optDir}}
		assertDoctorWarnings(t, ctx, CheckBrokenOptSymlinks, "broken opt symlink")
	})
}

func TestCheckOrphanedSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("valid installed", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		cellarDir := filepath.Join(tmpDir, "Cellar") // Must be capital C for CheckOrphanedSymlinks
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(cellarDir, 0755)
		// Mock an installed package
		pkgDir := filepath.Join(cellarDir, "installed_pkg", "1.0.0")
		os.MkdirAll(pkgDir, 0755)
		
		target := filepath.Join(pkgDir, "bin_file")
		os.WriteFile(target, []byte("test"), 0644)
		os.Symlink(target, filepath.Join(binDir, "pkg_link"))
		
		ctx := &Context{
			Paths: config.Paths{Bin: binDir, Cellar: cellarDir},
			Packages: []cellar.InstalledPackage{
				{Name: "installed_pkg", Version: "1.0.0"},
			},
		}
		ctx.Cel = &cellar.Cellar{Path: cellarDir}
		
		assertDoctorWarnings(t, ctx, CheckOrphanedSymlinks, "")
	})

	t.Run("orphaned", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		cellarDir := filepath.Join(tmpDir, "Cellar") // Must be capital C for CheckOrphanedSymlinks
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(cellarDir, 0755)
		os.Symlink(filepath.Join(cellarDir, "missing_pkg", "1.0.0", "bin_file"), filepath.Join(binDir, "orphan_link"))
		
		ctx := &Context{
			Paths: config.Paths{Bin: binDir, Cellar: cellarDir},
			Cel:   &cellar.Cellar{Path: cellarDir},
		}
		assertDoctorWarnings(t, ctx, CheckOrphanedSymlinks, "orphaned symlink")
	})
}

func TestCheckMultipleVersions(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cellarDir := filepath.Join(tmpDir, "Cellar")

	t.Run("single version", func(t *testing.T) {
		t.Parallel()
		pkgDir := filepath.Join(cellarDir, "single_pkg", "1.0.0")
		os.MkdirAll(pkgDir, 0755)

		ctx := &Context{
			Cel: &cellar.Cellar{Path: cellarDir},
			Packages: []cellar.InstalledPackage{
				{Name: "single_pkg", Version: "1.0.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckMultipleVersions, "")
	})

	t.Run("multiple versions", func(t *testing.T) {
		t.Parallel()
		os.MkdirAll(filepath.Join(cellarDir, "multi_pkg", "1.0.0"), 0755)
		os.MkdirAll(filepath.Join(cellarDir, "multi_pkg", "2.0.0"), 0755)

		ctx := &Context{
			Cel: &cellar.Cellar{Path: cellarDir},
			Packages: []cellar.InstalledPackage{
				{Name: "multi_pkg", Version: "2.0.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckMultipleVersions, "2 versions installed")
	})
}

func TestCheckUnlinkedKegs(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	
	t.Run("unlinked normal formula", func(t *testing.T) {
		t.Parallel()
		tapDir := filepath.Join(tmpDir, "Taps")
		formPath := filepath.Join(tapDir, "core", "unlinked.yaml")
		os.MkdirAll(filepath.Dir(formPath), 0755)
		os.WriteFile(formPath, []byte("name: unlinked\nversion: 1.0.0\nurl:\n  darwin_arm64: https://test.com\ninstall:\n  type: binary\n"), 0644)

		ctx := &Context{
			Paths: config.Paths{Root: tmpDir, Opt: filepath.Join(tmpDir, "opt"), Taps: tapDir},
			Packages: []cellar.InstalledPackage{
				{Name: "unlinked", Version: "1.0.0"},
			},
		}
		ctx.Lnk = &linker.Linker{Paths: ctx.Paths}
		ctx.Loader = formula.NewLoader(tapDir)

		assertDoctorWarnings(t, ctx, CheckUnlinkedKegs, "is installed but not linked")
	})

	t.Run("unlinked keg-only formula", func(t *testing.T) {
		t.Parallel()
		tapDir := filepath.Join(tmpDir, "Taps")
		formPath := filepath.Join(tapDir, "core", "kegonly.yaml")
		os.MkdirAll(filepath.Dir(formPath), 0755)
		os.WriteFile(formPath, []byte("name: kegonly\nversion: 1.0.0\nkeg_only: true\nurl:\n  darwin_arm64: https://test.com\ninstall:\n  type: binary\n"), 0644)

		ctx := &Context{
			Paths: config.Paths{Root: tmpDir, Opt: filepath.Join(tmpDir, "opt"), Taps: tapDir},
			Packages: []cellar.InstalledPackage{
				{Name: "kegonly", Version: "1.0.0"},
			},
		}
		ctx.Lnk = &linker.Linker{Paths: ctx.Paths}
		ctx.Loader = formula.NewLoader(tapDir)

		if _, err := ctx.Loader.LoadByName("kegonly"); err != nil {
			t.Fatalf("Failed to load: %v", err)
		}

		assertDoctorWarnings(t, ctx, CheckUnlinkedKegs, "")
	})
}

func TestCheckPinnedFormulas(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cellarDir := filepath.Join(tmpDir, "cellar")

	t.Run("no pinned", func(t *testing.T) {
		t.Parallel()
		ctx := &Context{
			Cel: &cellar.Cellar{Path: cellarDir},
			Packages: []cellar.InstalledPackage{
				{Name: "unpinned_pkg", Version: "1.0.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckPinnedFormulas, "")
	})

	t.Run("pinned", func(t *testing.T) {
		t.Parallel()
		pkgDir := filepath.Join(cellarDir, "pinned_pkg")
		os.MkdirAll(pkgDir, 0755)
		// Fake an installation so IsInstalled returns true
		os.MkdirAll(filepath.Join(pkgDir, "1.0.0"), 0755)
		os.WriteFile(filepath.Join(pkgDir, "PINNED"), []byte(""), 0644)

		ctx := &Context{
			Cel: &cellar.Cellar{Path: cellarDir},
			Packages: []cellar.InstalledPackage{
				{Name: "pinned_pkg", Version: "1.0.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckPinnedFormulas, "formulas are pinned")
	})
}

func TestCheckCaskSHA256(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		casks    []*cask.Cask
		wantWarn string
	}{
		{
			name: "valid sha256",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("a", 64)},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid sha256 length",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": "abc"},
				},
			},
			wantWarn: "cask foo: SHA256 for darwin_arm64 has wrong length (3, expected 64)",
		},
		{
			name: "invalid sha256 characters",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("z", 64)},
				},
			},
			wantWarn: "cask foo: SHA256 for darwin_arm64 contains non-hex character \"z\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &Context{Casks: tt.casks}
			assertDoctorWarnings(t, ctx, CheckCaskSHA256, tt.wantWarn)
		})
	}
}

func TestCheckCaskSHA512(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		casks    []*cask.Cask
		wantWarn string
	}{
		{
			name: "valid sha512",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("a", 128)},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid sha512 length",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": "abc"},
				},
			},
			wantWarn: "cask foo: SHA512 for darwin_arm64 has wrong length (3, expected 128)",
		},
		{
			name: "invalid sha512 characters",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("z", 128)},
				},
			},
			wantWarn: "cask foo: SHA512 for darwin_arm64 contains non-hex character \"z\"",
		},
		{
			name: "missing sha512 when sha256 is present",
			casks: []*cask.Cask{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("a", 64)},
				},
			},
			wantWarn: "cask foo: missing SHA512 metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &Context{Casks: tt.casks}
			assertDoctorWarnings(t, ctx, CheckCaskSHA512, tt.wantWarn)
		})
	}
}

func TestCheckFormulaSHA512(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		formulas []*formula.Formula
		wantWarn string
	}{
		{
			name: "valid sha512",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("a", 128)},
				},
			},
			wantWarn: "",
		},
		{
			name: "invalid sha512 length",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": "abc"},
				},
			},
			wantWarn: "SHA512 for darwin_arm64 has wrong length (3, expected 128)",
		},
		{
			name: "invalid sha512 characters",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA512: map[string]string{"darwin_arm64": strings.Repeat("z", 128)},
				},
			},
			wantWarn: "SHA512 for darwin_arm64 contains non-hex character \"z\"",
		},
		{
			name: "missing sha512 when sha256 is present",
			formulas: []*formula.Formula{
				{
					Name:   "foo",
					SHA256: map[string]string{"darwin_arm64": strings.Repeat("a", 64)},
				},
			},
			wantWarn: "formula foo: missing SHA512 metadata",
		},
		{
			name: "valid bottle sha512",
			formulas: []*formula.Formula{
				{
					Name: "foo",
					Bottle: map[string]formula.BottleSpec{
						"darwin_arm64": {
							SHA256: strings.Repeat("a", 64),
							SHA512: strings.Repeat("a", 128),
						},
					},
				},
			},
			wantWarn: "",
		},
		{
			name: "missing bottle sha512",
			formulas: []*formula.Formula{
				{
					Name: "foo",
					Bottle: map[string]formula.BottleSpec{
						"darwin_arm64": {
							SHA256: strings.Repeat("a", 64),
						},
					},
				},
			},
			wantWarn: "formula foo: bottle for darwin_arm64 missing SHA512",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &Context{Formulas: tt.formulas}
			assertDoctorWarnings(t, ctx, CheckFormulaSHA512, tt.wantWarn)
		})
	}
}

func TestCheckSymlinkTargets(t *testing.T) {
	t.Parallel()

	t.Run("valid internal symlink", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		root := filepath.Join(tmpDir, "root")
		binDir := filepath.Join(root, "bin")
		outsideDir := filepath.Join(tmpDir, "outside")
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(outsideDir, 0755)

		target := filepath.Join(root, "internal_file")
		os.WriteFile(target, []byte("test"), 0644)
		os.Symlink(target, filepath.Join(binDir, "internal_link"))
		
		ctx := &Context{Paths: config.Paths{Root: root, Bin: binDir}}
		assertDoctorWarnings(t, ctx, CheckSymlinkTargets, "")
	})

	t.Run("escaping symlink", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		root := filepath.Join(tmpDir, "root")
		binDir := filepath.Join(root, "bin")
		outsideDir := filepath.Join(tmpDir, "outside")
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(outsideDir, 0755)

		target := filepath.Join(outsideDir, "outside_file")
		os.WriteFile(target, []byte("test"), 0644)
		os.Symlink(target, filepath.Join(binDir, "escaping_link"))
		
		ctx := &Context{Paths: config.Paths{Root: root, Bin: binDir}}
		assertDoctorWarnings(t, ctx, CheckSymlinkTargets, "symlink escapes grew prefix")
	})
}

func TestCheckCellarPermissions(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	t.Run("safe permissions", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(tmpDir, "safe_keg")
		binDir := filepath.Join(kegPath, "bin")
		os.MkdirAll(binDir, 0755)
		binFile := filepath.Join(binDir, "exe")
		os.WriteFile(binFile, []byte("test"), 0755)

		ctx := &Context{
			Packages: []cellar.InstalledPackage{
				{Name: "safe", Version: "1.0", Path: kegPath},
			},
		}
		assertDoctorWarnings(t, ctx, CheckCellarPermissions, "")
	})

	t.Run("world-writable keg", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(tmpDir, "unsafe_keg")
		os.MkdirAll(kegPath, 0777)
		os.Chmod(kegPath, 0777)

		ctx := &Context{
			Packages: []cellar.InstalledPackage{
				{Name: "unsafe", Version: "1.0", Path: kegPath},
			},
		}
		assertDoctorWarnings(t, ctx, CheckCellarPermissions, "is world-writable")
	})

	t.Run("world-writable bin", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(tmpDir, "unsafe_bin_keg")
		binDir := filepath.Join(kegPath, "bin")
		os.MkdirAll(binDir, 0755)
		binFile := filepath.Join(binDir, "exe")
		os.WriteFile(binFile, []byte("test"), 0777)
		os.Chmod(binFile, 0777)

		ctx := &Context{
			Packages: []cellar.InstalledPackage{
				{Name: "unsafe_bin", Version: "1.0", Path: kegPath},
			},
		}
		assertDoctorWarnings(t, ctx, CheckCellarPermissions, "is world-writable")
	})
}

func TestCheckIncompleteInstalls(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	celPath := filepath.Join(tmpDir, "Cellar")
	os.MkdirAll(celPath, 0755)
	
	t.Run("complete install", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(celPath, "complete", "1.0")
		os.MkdirAll(kegPath, 0755)
		snapshot.Save(&snapshot.Manifest{Name: "complete", Version: "1.0"}, kegPath)

		ctx := &Context{
			Cel: &cellar.Cellar{Path: celPath},
			Packages: []cellar.InstalledPackage{
				{Name: "complete", Version: "1.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckIncompleteInstalls, "")
	})

	t.Run("incomplete install", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(celPath, "incomplete", "1.0")
		os.MkdirAll(kegPath, 0755)

		ctx := &Context{
			Cel: &cellar.Cellar{Path: celPath},
			Packages: []cellar.InstalledPackage{
				{Name: "incomplete", Version: "1.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckIncompleteInstalls, "installation manifest missing")
	})
}

func TestCheckSnapshotIntegrity(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	celPath := filepath.Join(tmpDir, "Cellar")
	os.MkdirAll(celPath, 0755)

	t.Run("integrity ok", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(celPath, "okpkg", "1.0")
		os.MkdirAll(kegPath, 0755)
		
		filePath := filepath.Join(kegPath, "file.txt")
		os.WriteFile(filePath, []byte("hello"), 0644)
		info, _ := os.Stat(filePath)
		
		manifest := &snapshot.Manifest{
			Name: "okpkg", Version: "1.0",
			Files: []snapshot.FileEntry{
				{Path: "file.txt", Size: info.Size(), Mode: info.Mode()},
			},
		}
		snapshot.Save(manifest, kegPath)
		
		ctx := &Context{
			Cel: &cellar.Cellar{Path: celPath},
			Packages: []cellar.InstalledPackage{
				{Name: "okpkg", Version: "1.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckSnapshotIntegrity, "")
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(celPath, "missingpkg", "1.0")
		os.MkdirAll(kegPath, 0755)
		
		manifest := &snapshot.Manifest{
			Name: "missingpkg", Version: "1.0",
			Files: []snapshot.FileEntry{
				{Path: "file.txt", Size: 5, Mode: 0644},
			},
		}
		snapshot.Save(manifest, kegPath)
		
		ctx := &Context{
			Cel: &cellar.Cellar{Path: celPath},
			Packages: []cellar.InstalledPackage{
				{Name: "missingpkg", Version: "1.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckSnapshotIntegrity, "missing file: file.txt")
	})

	t.Run("unexpected file", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(celPath, "addedpkg", "1.0")
		os.MkdirAll(kegPath, 0755)
		
		filePath := filepath.Join(kegPath, "extra.txt")
		os.WriteFile(filePath, []byte("extra"), 0644)
		
		manifest := &snapshot.Manifest{
			Name: "addedpkg", Version: "1.0",
			Files: []snapshot.FileEntry{},
		}
		snapshot.Save(manifest, kegPath)
		
		ctx := &Context{
			Cel: &cellar.Cellar{Path: celPath},
			Packages: []cellar.InstalledPackage{
				{Name: "addedpkg", Version: "1.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckSnapshotIntegrity, "unexpected file: extra.txt")
	})

	t.Run("modified file", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(celPath, "modpkg", "1.0")
		os.MkdirAll(kegPath, 0755)
		
		filePath := filepath.Join(kegPath, "file.txt")
		os.WriteFile(filePath, []byte("modified"), 0644)
		info, _ := os.Stat(filePath)
		
		manifest := &snapshot.Manifest{
			Name: "modpkg", Version: "1.0",
			Files: []snapshot.FileEntry{
				{Path: "file.txt", Size: info.Size(), Mode: info.Mode(), SHA256: "fakehash"},
			},
		}
		snapshot.Save(manifest, kegPath)
		
		ctx := &Context{
			Cel: &cellar.Cellar{Path: celPath},
			Packages: []cellar.InstalledPackage{
				{Name: "modpkg", Version: "1.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckSnapshotIntegrity, "modified: file.txt")
	})
    
    t.Run("aggregate mismatch", func(t *testing.T) {
		t.Parallel()
		kegPath := filepath.Join(celPath, "aggpkg", "1.0")
		os.MkdirAll(kegPath, 0755)
		
		filePath := filepath.Join(kegPath, "file.txt")
		os.WriteFile(filePath, []byte("hello"), 0644)
		info, _ := os.Stat(filePath)
		
		manifest := &snapshot.Manifest{
			Name: "aggpkg", Version: "1.0",
			Files: []snapshot.FileEntry{
				{Path: "file.txt", Size: info.Size(), Mode: info.Mode()},
			},
            KegSHA256: "wronghash",
		}
		snapshot.Save(manifest, kegPath)
		
		ctx := &Context{
			Cel: &cellar.Cellar{Path: celPath},
			Packages: []cellar.InstalledPackage{
				{Name: "aggpkg", Version: "1.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckSnapshotIntegrity, "aggregate SHA256 mismatch")
	})
}

func TestCheckSandbox(t *testing.T) {
	t.Parallel()
	ctx := &Context{}
	var warnings []string
	ctx.Warn = func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}
	
	CheckSandbox(ctx)
	
	if sandbox.IsSandboxed() {
		if len(warnings) > 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
	} else {
		if len(warnings) == 0 {
			t.Errorf("expected warning about sandboxing not available")
		} else if !strings.Contains(warnings[0], "Functional sandboxing is NOT available") {
			t.Errorf("expected warning about sandboxing not available, got %q", warnings[0])
		}
	}
}
