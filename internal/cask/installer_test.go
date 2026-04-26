package cask

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallApp(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "Applications")
	binDir := filepath.Join(tmpDir, "bin")
	stageDir := filepath.Join(tmpDir, "stage")

	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{
		AppDir: appDir,
		BinDir: binDir,
	}

	// Create a dummy app in stageDir
	dummyApp := filepath.Join(stageDir, "TestApp.app")
	if err := os.MkdirAll(filepath.Join(dummyApp, "Contents"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dummyApp, "Contents", "Info.plist"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Success: App found at top level of stageDir
	t.Run("TopLevel", func(t *testing.T) {
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
		nestedDir := filepath.Join(stageDir, "subdir")
		if err := os.MkdirAll(nestedDir, 0755); err != nil {
			t.Fatal(err)
		}
		nestedApp := filepath.Join(nestedDir, "NestedApp.app")
		if err := os.MkdirAll(filepath.Join(nestedApp, "Contents"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nestedApp, "Contents", "Info.plist"), []byte("nested"), 0644); err != nil {
			t.Fatal(err)
		}

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
		// TestApp.app already exists in appDir from previous subtest
		dest, err := inst.InstallApp(stageDir, "TestApp.app")
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}
		if _, err := os.Stat(dest); err != nil {
			t.Errorf("app missing after overwrite: %v", err)
		}
	})

	// Failure: Not a .app bundle
	t.Run("NotApp", func(t *testing.T) {
		_, err := inst.InstallApp(stageDir, "NotAnApp")
		if err == nil || !strings.Contains(err.Error(), "is not a .app bundle") {
			t.Errorf("expected 'not a .app bundle' error, got %v", err)
		}
	})

	// Failure: App name contains path traversal
	t.Run("PathTraversal", func(t *testing.T) {
		_, err := inst.InstallApp(stageDir, "../Other.app")
		if err == nil {
			t.Error("expected error for path traversal in app name")
		}
	})

	// Failure: App not found in stageDir
	t.Run("NotFound", func(t *testing.T) {
		_, err := inst.InstallApp(stageDir, "Missing.app")
		if err == nil || !strings.Contains(err.Error(), "could not find") {
			t.Errorf("expected 'could not find' error, got %v", err)
		}
	})

	// Failure: App resolves outside stageDir (symlink attack)
	t.Run("SymlinkEscape", func(t *testing.T) {
		outsideDir := t.TempDir()
		outsideApp := filepath.Join(outsideDir, "Outside.app")
		if err := os.MkdirAll(outsideApp, 0755); err != nil {
			t.Fatal(err)
		}

		symlinkApp := filepath.Join(stageDir, "Evil.app")
		if err := os.Symlink(outsideApp, symlinkApp); err != nil {
			t.Fatal(err)
		}

		_, err := inst.InstallApp(stageDir, "Evil.app")
		if err == nil || !strings.Contains(err.Error(), "resolves outside staging directory") {
			t.Errorf("expected 'resolves outside staging directory' error, got %v", err)
		}
	})
}

func TestUninstallApp(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "Applications")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{AppDir: appDir}

	// Create an app to uninstall
	appName := "ToUninstall.app"
	appPath := filepath.Join(appDir, appName)
	if err := os.MkdirAll(appPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Success: App exists and is removed
	t.Run("Success", func(t *testing.T) {
		if err := inst.UninstallApp(appName); err != nil {
			t.Fatalf("UninstallApp failed: %v", err)
		}
		if _, err := os.Stat(appPath); !os.IsNotExist(err) {
			t.Errorf("app still exists after uninstall")
		}
	})

	// Success: App does not exist (no-op)
	t.Run("NoOp", func(t *testing.T) {
		if err := inst.UninstallApp("Missing.app"); err != nil {
			t.Fatalf("UninstallApp failed: %v", err)
		}
	})

	// Failure: Invalid app name
	t.Run("InvalidName", func(t *testing.T) {
		if err := inst.UninstallApp("path/to/bad.app"); err == nil {
			t.Error("expected error for invalid app name")
		}
	})
}

func TestLinkBin(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{BinDir: binDir}
	targetFile := filepath.Join(tmpDir, "some-binary")
	if err := os.WriteFile(targetFile, []byte("echo hi"), 0755); err != nil {
		t.Fatal(err)
	}

	// Success: Create a symlink
	t.Run("Success", func(t *testing.T) {
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
		newTarget := filepath.Join(tmpDir, "other-binary")
		if err := os.WriteFile(newTarget, []byte("hi"), 0755); err != nil {
			t.Fatal(err)
		}
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
		regFile := filepath.Join(binDir, "regular-file")
		if err := os.WriteFile(regFile, []byte("not a link"), 0644); err != nil {
			t.Fatal(err)
		}
		err := inst.LinkBin("regular-file", targetFile)
		if err == nil || !strings.Contains(err.Error(), "refusing to overwrite regular file") {
			t.Errorf("expected 'refusing to overwrite regular file' error, got %v", err)
		}
	})

	// Failure: Overwrite a directory
	t.Run("RefuseDir", func(t *testing.T) {
		dirPath := filepath.Join(binDir, "some-dir")
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			t.Fatal(err)
		}
		err := inst.LinkBin("some-dir", targetFile)
		if err == nil || !strings.Contains(err.Error(), "refusing to overwrite directory") {
			t.Errorf("expected 'refusing to overwrite directory' error, got %v", err)
		}
	})

	// Failure: Target contains path traversal
	t.Run("PathTraversalTarget", func(t *testing.T) {
		err := inst.LinkBin("badlink", "../outside")
		if err == nil || !strings.Contains(err.Error(), "contains path traversal") {
			t.Errorf("expected 'path traversal' error, got %v", err)
		}
	})

	// Failure: Invalid binary name
	t.Run("InvalidName", func(t *testing.T) {
		if err := inst.LinkBin("in valid", targetFile); err == nil {
			t.Error("expected error for invalid binary name")
		}
	})
}

func TestUnlinkBin(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	inst := &Installer{BinDir: binDir}

	// Create a link to unlink
	linkName := "to-unlink"
	linkPath := filepath.Join(binDir, linkName)
	if err := os.Symlink("/dev/null", linkPath); err != nil {
		t.Fatal(err)
	}

	// Success: Symlink exists and is removed
	t.Run("Success", func(t *testing.T) {
		if err := inst.UnlinkBin(linkName); err != nil {
			t.Fatalf("UnlinkBin failed: %v", err)
		}
		if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
			t.Errorf("link still exists after unlink")
		}
	})

	// Success: Symlink does not exist (no-op)
	t.Run("NoOp", func(t *testing.T) {
		if err := inst.UnlinkBin("missing-link"); err != nil {
			t.Fatalf("UnlinkBin failed: %v", err)
		}
	})

	// Failure: Path is not a symlink
	t.Run("NotSymlink", func(t *testing.T) {
		regFile := filepath.Join(binDir, "not-a-link")
		if err := os.WriteFile(regFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
		err := inst.UnlinkBin("not-a-link")
		if err == nil || !strings.Contains(err.Error(), "refusing to remove non-symlink") {
			t.Errorf("expected 'refusing to remove non-symlink' error, got %v", err)
		}
	})
}
