package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/pkg/cellar"
	"github.com/homegrew/grew/pkg/formula"
)

func TestCheckCellarOrphans(t *testing.T) {
	t.Parallel()

	t.Run("no orphans", func(t *testing.T) {
		t.Parallel()
		tapDir := t.TempDir()
		formPath := filepath.Join(tapDir, "homegrew", "homegrew-taps", "core", "mypkg.yaml")
		os.MkdirAll(filepath.Dir(formPath), 0755)
		os.WriteFile(formPath, []byte("name: mypkg\nversion: 1.0.0\nurl:\n  darwin_arm64: https://test.com\ninstall:\n  type: binary\n"), 0644)

		ctx := &Context{
			Loader: formula.NewLoader(tapDir),
			Packages: []cellar.InstalledPackage{
				{Name: "mypkg", Version: "1.0.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckCellarOrphans, "")
	})

	t.Run("orphan keg", func(t *testing.T) {
		t.Parallel()
		tapDir := t.TempDir() // empty tap — no formulas

		ctx := &Context{
			Loader: formula.NewLoader(tapDir),
			Packages: []cellar.InstalledPackage{
				{Name: "ghost", Version: "2.3.4"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckCellarOrphans, "formula not found in any tap")
	})

	t.Run("mixed installed and orphan", func(t *testing.T) {
		t.Parallel()
		tapDir := t.TempDir()
		formPath := filepath.Join(tapDir, "homegrew", "homegrew-taps", "core", "real.yaml")
		os.MkdirAll(filepath.Dir(formPath), 0755)
		os.WriteFile(formPath, []byte("name: real\nversion: 1.0.0\nurl:\n  darwin_arm64: https://test.com\ninstall:\n  type: binary\n"), 0644)

		ctx := &Context{
			Loader: formula.NewLoader(tapDir),
			Packages: []cellar.InstalledPackage{
				{Name: "real", Version: "1.0.0"},
				{Name: "vanished", Version: "0.9.0"},
			},
		}
		assertDoctorWarnings(t, ctx, CheckCellarOrphans, "vanished")
	})
}
