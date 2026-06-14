package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/pkg/cellar"
	"github.com/homegrew/grew/pkg/config"
)

func TestCheckStaleVersionSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("current symlink — no warning", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		binDir := filepath.Join(tmp, "bin")
		cellarDir := filepath.Join(tmp, "Cellar")
		kegBin := filepath.Join(cellarDir, "mypkg", "1.2.0", "bin")
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(kegBin, 0755)
		os.WriteFile(filepath.Join(kegBin, "mypkg"), []byte(""), 0755)
		os.Symlink(filepath.Join(kegBin, "mypkg"), filepath.Join(binDir, "mypkg"))

		ctx := &Context{
			Paths:    config.Paths{Bin: binDir, Lib: filepath.Join(tmp, "lib"), Include: filepath.Join(tmp, "include"), Cellar: cellarDir},
			Cel:      &cellar.Cellar{Path: cellarDir},
			Packages: []cellar.InstalledPackage{{Name: "mypkg", Version: "1.2.0"}},
		}
		assertDoctorWarnings(t, ctx, CheckStaleVersionSymlinks, "")
	})

	t.Run("stale symlink after upgrade — warns", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		binDir := filepath.Join(tmp, "bin")
		cellarDir := filepath.Join(tmp, "Cellar")
		// Old keg (the one the symlink still points to)
		oldKegBin := filepath.Join(cellarDir, "mypkg", "1.0.0", "bin")
		// New keg (the currently installed version)
		newKegBin := filepath.Join(cellarDir, "mypkg", "2.0.0", "bin")
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(oldKegBin, 0755)
		os.MkdirAll(newKegBin, 0755)
		os.WriteFile(filepath.Join(oldKegBin, "mypkg"), []byte(""), 0755)
		os.WriteFile(filepath.Join(newKegBin, "mypkg"), []byte(""), 0755)
		// Symlink still points at old version
		os.Symlink(filepath.Join(oldKegBin, "mypkg"), filepath.Join(binDir, "mypkg"))

		ctx := &Context{
			Paths:    config.Paths{Bin: binDir, Lib: filepath.Join(tmp, "lib"), Include: filepath.Join(tmp, "include"), Cellar: cellarDir},
			Cel:      &cellar.Cellar{Path: cellarDir},
			Packages: []cellar.InstalledPackage{{Name: "mypkg", Version: "2.0.0"}},
		}
		assertDoctorWarnings(t, ctx, CheckStaleVersionSymlinks, "stale symlink")
	})

	t.Run("symlink outside Cellar — ignored", func(t *testing.T) {
		t.Parallel()
		tmp := t.TempDir()
		binDir := filepath.Join(tmp, "bin")
		cellarDir := filepath.Join(tmp, "Cellar")
		externalBin := filepath.Join(tmp, "external", "bin")
		os.MkdirAll(binDir, 0755)
		os.MkdirAll(externalBin, 0755)
		os.WriteFile(filepath.Join(externalBin, "tool"), []byte(""), 0755)
		os.Symlink(filepath.Join(externalBin, "tool"), filepath.Join(binDir, "tool"))

		ctx := &Context{
			Paths: config.Paths{Bin: binDir, Lib: filepath.Join(tmp, "lib"), Include: filepath.Join(tmp, "include"), Cellar: cellarDir},
			Cel:   &cellar.Cellar{Path: cellarDir},
		}
		assertDoctorWarnings(t, ctx, CheckStaleVersionSymlinks, "")
	})
}
