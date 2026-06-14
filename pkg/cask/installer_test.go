package cask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallApp(t *testing.T) {
	t.Parallel()

	// Success: App found at top level of stageDir
	t.Run("TopLevel", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		appDir := filepath.Join(tmpDir, "Applications")
		stageDir := filepath.Join(tmpDir, "stage")
		os.MkdirAll(appDir, 0755)
		os.MkdirAll(stageDir, 0755)

		inst := &Installer{AppDir: appDir}
		dummyApp := filepath.Join(stageDir, "TestApp.app")
		os.MkdirAll(filepath.Join(dummyApp, "Contents"), 0755)
		os.WriteFile(filepath.Join(dummyApp, "Contents", "Info.plist"), []byte("test"), 0644)

		dest, err := inst.InstallApp(stageDir, "TestApp.app")
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}
		expectedDest := filepath.Join(appDir, "TestApp.app")
		if dest != expectedDest {
			t.Errorf("expected dest %q, got %q", expectedDest, dest)
		}
		if _, err := os.Stat(filepath.Join(dest, "Contents", "Info.plist")); err != nil {
			t.Errorf("app not copied correctly: %v", err)
		}
	})

	// Success: App found in a sub-directory of stageDir
	t.Run("Nested", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		appDir := filepath.Join(tmpDir, "Applications")
		stageDir := filepath.Join(tmpDir, "stage")
		os.MkdirAll(appDir, 0755)
		os.MkdirAll(stageDir, 0755)

		inst := &Installer{AppDir: appDir}
		nestedDir := filepath.Join(stageDir, "subdir")
		os.MkdirAll(nestedDir, 0755)
		nestedApp := filepath.Join(nestedDir, "NestedApp.app")
		os.MkdirAll(filepath.Join(nestedApp, "Contents"), 0755)
		os.WriteFile(filepath.Join(nestedApp, "Contents", "Info.plist"), []byte("nested"), 0644)

		dest, err := inst.InstallApp(stageDir, "NestedApp.app")
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}
		expectedDest := filepath.Join(appDir, "NestedApp.app")
		if dest != expectedDest {
			t.Errorf("expected dest %q, got %q", expectedDest, dest)
		}
	})

	// Success: Overwriting an existing app
	t.Run("Overwrite", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		appDir := filepath.Join(tmpDir, "Applications")
		stageDir := filepath.Join(tmpDir, "stage")
		os.MkdirAll(appDir, 0755)
		os.MkdirAll(stageDir, 0755)

		inst := &Installer{AppDir: appDir}

		// Create existing app in appDir
		existingApp := filepath.Join(appDir, "TestApp.app")
		os.MkdirAll(existingApp, 0755)
		os.WriteFile(filepath.Join(existingApp, "old"), []byte("old"), 0644)

		// Create new app in stageDir
		dummyApp := filepath.Join(stageDir, "TestApp.app")
		os.MkdirAll(dummyApp, 0755)
		os.WriteFile(filepath.Join(dummyApp, "new"), []byte("new"), 0644)

		dest, err := inst.InstallApp(stageDir, "TestApp.app")
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dest, "new")); err != nil {
			t.Errorf("new app file missing after overwrite: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dest, "old")); !os.IsNotExist(err) {
			t.Errorf("old app file still exists after overwrite")
		}
	})

	// Failure: Not a .app bundle
	t.Run("NotApp", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{AppDir: t.TempDir()}
		_, err := inst.InstallApp(t.TempDir(), "NotAnApp")
		if err == nil || !strings.Contains(err.Error(), "is not a .app bundle") {
			t.Errorf("expected 'not a .app bundle' error, got %v", err)
		}
	})

	// Failure: App name contains path traversal
	t.Run("PathTraversal", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{AppDir: t.TempDir()}
		_, err := inst.InstallApp(t.TempDir(), "../Other.app")
		if err == nil {
			t.Error("expected error for path traversal in app name")
		}
	})

	// Failure: App not found in stageDir
	t.Run("NotFound", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{AppDir: t.TempDir()}
		_, err := inst.InstallApp(t.TempDir(), "Missing.app")
		if err == nil || !strings.Contains(err.Error(), "could not find") {
			t.Errorf("expected 'could not find' error, got %v", err)
		}
	})

	// Failure: App resolves outside stageDir (symlink attack)
	t.Run("SymlinkEscape", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stageDir := filepath.Join(tmpDir, "stage")
		os.MkdirAll(stageDir, 0755)

		outsideDir := t.TempDir()
		outsideApp := filepath.Join(outsideDir, "Outside.app")
		os.MkdirAll(outsideApp, 0755)

		symlinkApp := filepath.Join(stageDir, "Evil.app")
		if err := os.Symlink(outsideApp, symlinkApp); err != nil {
			t.Fatal(err)
		}

		inst := &Installer{AppDir: t.TempDir()}
		_, err := inst.InstallApp(stageDir, "Evil.app")
		if err == nil || !strings.Contains(err.Error(), "resolves outside staging directory") {
			t.Errorf("expected 'resolves outside staging directory' error, got %v", err)
		}
	})
}

func TestUninstallApp(t *testing.T) {
	t.Parallel()

	// Success: App exists and is removed
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		appDir := t.TempDir()
		inst := &Installer{AppDir: appDir}
		appName := "ToUninstall.app"
		appPath := filepath.Join(appDir, appName)
		os.MkdirAll(appPath, 0755)

		if err := inst.UninstallApp(appName); err != nil {
			t.Fatalf("UninstallApp failed: %v", err)
		}
		if _, err := os.Stat(appPath); !os.IsNotExist(err) {
			t.Errorf("app still exists after uninstall")
		}
	})

	// Success: App does not exist (no-op)
	t.Run("NoOp", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{AppDir: t.TempDir()}
		if err := inst.UninstallApp("Missing.app"); err != nil {
			t.Fatalf("UninstallApp failed: %v", err)
		}
	})

	// Failure: Invalid app name
	t.Run("InvalidName", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{AppDir: t.TempDir()}
		if err := inst.UninstallApp("path/to/bad.app"); err == nil {
			t.Error("expected error for invalid app name")
		}
	})
}

func TestLinkBin(t *testing.T) {
	t.Parallel()

	// Success: Create a symlink
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		os.MkdirAll(binDir, 0755)
		inst := &Installer{BinDir: binDir}
		targetFile := filepath.Join(tmpDir, "some-binary")
		os.WriteFile(targetFile, []byte("echo hi"), 0755)

		if err := inst.LinkBin("mybin", targetFile); err != nil {
			t.Fatalf("LinkBin failed: %v", err)
		}
		linkPath := filepath.Join(binDir, "mybin")
		gotTarget, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("Readlink failed: %v", err)
		}
		if gotTarget != targetFile {
			t.Errorf("expected link target %q, got %q", targetFile, gotTarget)
		}
	})

	// Success: Overwrite an existing symlink
	t.Run("OverwriteLink", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		os.MkdirAll(binDir, 0755)
		inst := &Installer{BinDir: binDir}

		// Initial link
		oldTarget := filepath.Join(tmpDir, "old")
		os.WriteFile(oldTarget, []byte("old"), 0755)
		inst.LinkBin("mybin", oldTarget)

		newTarget := filepath.Join(tmpDir, "new")
		os.WriteFile(newTarget, []byte("new"), 0755)
		if err := inst.LinkBin("mybin", newTarget); err != nil {
			t.Fatalf("LinkBin failed: %v", err)
		}
		gotTarget, _ := os.Readlink(filepath.Join(binDir, "mybin"))
		if gotTarget != newTarget {
			t.Errorf("expected new link target %q, got %q", newTarget, gotTarget)
		}
	})

	// Failure: Overwrite a regular file
	t.Run("RefuseRegularFile", func(t *testing.T) {
		t.Parallel()
		binDir := t.TempDir()
		inst := &Installer{BinDir: binDir}
		regFile := filepath.Join(binDir, "regular-file")
		os.WriteFile(regFile, []byte("not a link"), 0644)

		err := inst.LinkBin("regular-file", "/tmp/target")
		if err == nil || !strings.Contains(err.Error(), "refusing to overwrite regular file") {
			t.Errorf("expected 'refusing to overwrite regular file' error, got %v", err)
		}
	})

	// Failure: Overwrite a directory
	t.Run("RefuseDir", func(t *testing.T) {
		t.Parallel()
		binDir := t.TempDir()
		inst := &Installer{BinDir: binDir}
		dirPath := filepath.Join(binDir, "some-dir")
		os.MkdirAll(dirPath, 0755)

		err := inst.LinkBin("some-dir", "/tmp/target")
		if err == nil || !strings.Contains(err.Error(), "refusing to overwrite directory") {
			t.Errorf("expected 'refusing to overwrite directory' error, got %v", err)
		}
	})

	// Failure: Target contains path traversal
	t.Run("PathTraversalTarget", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{BinDir: t.TempDir()}
		err := inst.LinkBin("badlink", "../outside")
		if err == nil || !strings.Contains(err.Error(), "contains path traversal") {
			t.Errorf("expected 'path traversal' error, got %v", err)
		}
	})

	// Failure: Invalid binary name
	t.Run("InvalidName", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{BinDir: t.TempDir()}
		if err := inst.LinkBin("in valid", "/tmp/target"); err == nil {
			t.Error("expected error for invalid binary name")
		}
	})
}

func TestUnlinkBin(t *testing.T) {
	t.Parallel()

	// Success: Symlink exists and is removed
	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		binDir := t.TempDir()
		inst := &Installer{BinDir: binDir}
		linkName := "to-unlink"
		linkPath := filepath.Join(binDir, linkName)
		os.Symlink("/dev/null", linkPath)

		if err := inst.UnlinkBin(linkName); err != nil {
			t.Fatalf("UnlinkBin failed: %v", err)
		}
		if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
			t.Errorf("link still exists after unlink")
		}
	})

	// Success: Symlink does not exist (no-op)
	t.Run("NoOp", func(t *testing.T) {
		t.Parallel()
		inst := &Installer{BinDir: t.TempDir()}
		if err := inst.UnlinkBin("missing-link"); err != nil {
			t.Fatalf("UnlinkBin failed: %v", err)
		}
	})

	// Failure: Path is not a symlink
	t.Run("NotSymlink", func(t *testing.T) {
		t.Parallel()
		binDir := t.TempDir()
		inst := &Installer{BinDir: binDir}
		regFile := filepath.Join(binDir, "not-a-link")
		os.WriteFile(regFile, []byte("test"), 0644)

		err := inst.UnlinkBin("not-a-link")
		if err == nil || !strings.Contains(err.Error(), "refusing to remove non-symlink") {
			t.Errorf("expected 'refusing to remove non-symlink' error, got %v", err)
		}
	})
}

func TestInstallFont(t *testing.T) {
	t.Parallel()

	// Success: font found at the exact relative path inside a nested archive dir.
	t.Run("NestedPath", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fontDir := filepath.Join(tmpDir, "Fonts")
		stageDir := filepath.Join(tmpDir, "stage")
		fontRel := "pkg-1.0/otfs/My Font-Regular.otf"
		srcFont := filepath.Join(stageDir, filepath.FromSlash(fontRel))
		os.MkdirAll(filepath.Dir(srcFont), 0755)
		os.WriteFile(srcFont, []byte("FONTDATA"), 0644)

		inst := &Installer{FontDir: fontDir}
		dest, err := inst.InstallFont(stageDir, fontRel)
		if err != nil {
			t.Fatalf("InstallFont failed: %v", err)
		}
		expectedDest := filepath.Join(fontDir, "My Font-Regular.otf")
		if dest != expectedDest {
			t.Errorf("expected dest %q, got %q", expectedDest, dest)
		}
		data, err := os.ReadFile(dest)
		if err != nil || string(data) != "FONTDATA" {
			t.Errorf("font not copied correctly: data=%q err=%v", data, err)
		}
	})

	// Success: exact path missing, located by base name elsewhere in the tree.
	t.Run("BasenameFallback", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fontDir := filepath.Join(tmpDir, "Fonts")
		stageDir := filepath.Join(tmpDir, "stage")
		actual := filepath.Join(stageDir, "elsewhere", "Cool.ttf")
		os.MkdirAll(filepath.Dir(actual), 0755)
		os.WriteFile(actual, []byte("x"), 0644)

		inst := &Installer{FontDir: fontDir}
		dest, err := inst.InstallFont(stageDir, "expected/path/Cool.ttf")
		if err != nil {
			t.Fatalf("InstallFont failed: %v", err)
		}
		if dest != filepath.Join(fontDir, "Cool.ttf") {
			t.Errorf("unexpected dest %q", dest)
		}
	})

	// Failure: unrecognized extension is rejected.
	t.Run("BadExtension", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		inst := &Installer{FontDir: filepath.Join(tmpDir, "Fonts")}
		if _, err := inst.InstallFont(filepath.Join(tmpDir, "stage"), "notafont.txt"); err == nil {
			t.Errorf("expected error for non-font extension")
		}
	})

	// Failure: font not present in the archive.
	t.Run("Missing", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stageDir := filepath.Join(tmpDir, "stage")
		os.MkdirAll(stageDir, 0755)
		inst := &Installer{FontDir: filepath.Join(tmpDir, "Fonts")}
		if _, err := inst.InstallFont(stageDir, "missing.otf"); err == nil {
			t.Errorf("expected error for missing font")
		}
	})

	// UninstallFont removes the installed file and is idempotent.
	t.Run("Uninstall", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		fontDir := filepath.Join(tmpDir, "Fonts")
		os.MkdirAll(fontDir, 0755)
		installed := filepath.Join(fontDir, "Gone.otf")
		os.WriteFile(installed, []byte("x"), 0644)

		inst := &Installer{FontDir: fontDir}
		if err := inst.UninstallFont("any/dir/Gone.otf"); err != nil {
			t.Fatalf("UninstallFont failed: %v", err)
		}
		if _, err := os.Stat(installed); !os.IsNotExist(err) {
			t.Errorf("font was not removed")
		}
		// Removing again is a no-op.
		if err := inst.UninstallFont("Gone.otf"); err != nil {
			t.Errorf("second UninstallFont should be no-op, got %v", err)
		}
	})
}

func TestInstallInstallerScript(t *testing.T) {
	t.Parallel()
	inst := &Installer{}

	// Sudo scripts are refused without ever running anything.
	t.Run("SudoRefused", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		err := inst.InstallInstallerScript(tmpDir, InstallerScript{Executable: "x.sh", Sudo: true}, tmpDir)
		if err == nil || !strings.Contains(err.Error(), "requires sudo") {
			t.Errorf("expected sudo refusal, got %v", err)
		}
	})

	// Missing executable name.
	t.Run("NoExecutable", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		if err := inst.InstallInstallerScript(tmpDir, InstallerScript{}, tmpDir); err == nil {
			t.Errorf("expected error for missing executable")
		}
	})

	// Executable not present in the staged archive.
	t.Run("Missing", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		stageDir := filepath.Join(tmpDir, "stage")
		os.MkdirAll(stageDir, 0755)
		err := inst.InstallInstallerScript(stageDir, InstallerScript{Executable: "absent.sh"}, tmpDir)
		if err == nil || !strings.Contains(err.Error(), "could not find") {
			t.Errorf("expected 'could not find' error, got %v", err)
		}
	})
}

func TestExpandPrefixVars(t *testing.T) {
	t.Parallel()
	got := expandPrefixVars([]string{"-p", "$HOMEGREW_PREFIX/anaconda3", "${HOMEGREW_PREFIX}/x", "plain"}, "/opt/homegrew")
	want := []string{"-p", "/opt/homegrew/anaconda3", "/opt/homegrew/x", "plain"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}
