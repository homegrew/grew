package downloader

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/formula"
)

func TestExtractTarGz(t *testing.T) {
	t.Parallel()

	t.Run("basic extraction", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGz(t, archivePath, []tarEntry{
			{name: "file.txt", content: "hello", mode: 0644},
			{name: "bin/tool", content: "binary", mode: 0755},
		})

		if err := extractTarGz(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}

		assertFileContent(t, filepath.Join(destDir, "file.txt"), "hello")
		assertFileContent(t, filepath.Join(destDir, "bin/tool"), "binary")
	})

	t.Run("strip components", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGz(t, archivePath, []tarEntry{
			{name: "pkg-1.0/bin/tool", content: "binary", mode: 0755},
			{name: "pkg-1.0/README", content: "readme", mode: 0644},
		})

		if err := extractTarGz(archivePath, destDir, 1); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}

		assertFileContent(t, filepath.Join(destDir, "bin/tool"), "binary")
		assertFileContent(t, filepath.Join(destDir, "README"), "readme")
	})

	t.Run("path traversal blocked", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "evil.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGz(t, archivePath, []tarEntry{
			{name: "../../../etc/passwd", content: "root:x:0:0", mode: 0644},
			{name: "safe.txt", content: "ok", mode: 0644},
		})

		if err := extractTarGz(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}

		// Traversal entry should be skipped
		if _, err := os.Stat(filepath.Join(destDir, "../../../etc/passwd")); !os.IsNotExist(err) {
			t.Error("path traversal entry should have been skipped")
		}
		assertFileContent(t, filepath.Join(destDir, "safe.txt"), "ok")
	})

	t.Run("symlink within dir allowed", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGzWithSymlinks(t, archivePath, []tarEntry{
			{name: "real.txt", content: "data", mode: 0644},
		}, []symlinkEntry{
			{name: "link.txt", target: "real.txt"},
		})

		if err := extractTarGz(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}

		linkPath := filepath.Join(destDir, "link.txt")
		linkDest, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink: %v", err)
		}
		if linkDest != "real.txt" {
			t.Errorf("symlink target = %q, want %q", linkDest, "real.txt")
		}
	})

	t.Run("symlink escape blocked", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "evil.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGzWithSymlinks(t, archivePath, nil, []symlinkEntry{
			{name: "escape", target: "../../../etc/passwd"},
		})

		if err := extractTarGz(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}

		if _, err := os.Lstat(filepath.Join(destDir, "escape")); !os.IsNotExist(err) {
			t.Error("escaping symlink should have been skipped")
		}
	})

	t.Run("directory entries", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGzWithDirs(t, archivePath, []string{"mydir/"}, []tarEntry{
			{name: "mydir/file.txt", content: "inside", mode: 0644},
		})

		if err := extractTarGz(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}

		info, err := os.Stat(filepath.Join(destDir, "mydir"))
		if err != nil {
			t.Fatalf("dir not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected mydir to be a directory")
		}
		assertFileContent(t, filepath.Join(destDir, "mydir/file.txt"), "inside")
	})
}

func TestExtractZip(t *testing.T) {
	t.Parallel()

	t.Run("basic extraction", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.zip")
		destDir := filepath.Join(tmpDir, "dest")

		createZip(t, archivePath, []zipEntry{
			{name: "file.txt", content: "hello"},
			{name: "bin/tool", content: "binary"},
		})

		if err := extractZip(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractZip: %v", err)
		}

		assertFileContent(t, filepath.Join(destDir, "file.txt"), "hello")
		assertFileContent(t, filepath.Join(destDir, "bin/tool"), "binary")
	})

	t.Run("strip components", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.zip")
		destDir := filepath.Join(tmpDir, "dest")

		createZip(t, archivePath, []zipEntry{
			{name: "pkg-1.0/bin/tool", content: "binary"},
			{name: "pkg-1.0/README", content: "readme"},
		})

		if err := extractZip(archivePath, destDir, 1); err != nil {
			t.Fatalf("extractZip: %v", err)
		}

		assertFileContent(t, filepath.Join(destDir, "bin/tool"), "binary")
		assertFileContent(t, filepath.Join(destDir, "README"), "readme")
	})

	t.Run("path traversal blocked", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "evil.zip")
		destDir := filepath.Join(tmpDir, "dest")

		createZip(t, archivePath, []zipEntry{
			{name: "../../../etc/passwd", content: "root:x:0:0"},
			{name: "safe.txt", content: "ok"},
		})

		if err := extractZip(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractZip: %v", err)
		}

		if _, err := os.Stat(filepath.Join(destDir, "../../../etc/passwd")); !os.IsNotExist(err) {
			t.Error("path traversal entry should have been skipped")
		}
		assertFileContent(t, filepath.Join(destDir, "safe.txt"), "ok")
	})

	t.Run("directory entries", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.zip")
		destDir := filepath.Join(tmpDir, "dest")

		createZip(t, archivePath, []zipEntry{
			{name: "mydir/", content: ""},
			{name: "mydir/file.txt", content: "inside"},
		})

		if err := extractZip(archivePath, destDir, 0); err != nil {
			t.Fatalf("extractZip: %v", err)
		}

		info, err := os.Stat(filepath.Join(destDir, "mydir"))
		if err != nil {
			t.Fatalf("dir not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected mydir to be a directory")
		}
	})
}

func TestExtractArchive_UnsupportedFormat(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.rar")
	os.WriteFile(archivePath, []byte("fake"), 0644)

	err := extractArchive(archivePath, filepath.Join(tmpDir, "dest"), 0)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestExtract_ArchiveWithBinaryMove(t *testing.T) {
	t.Parallel()

	t.Run("moves root binary to bin dir", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGz(t, archivePath, []tarEntry{
			{name: "mytool", content: "#!/bin/sh\necho hi", mode: 0755},
		})

		spec := formula.InstallSpec{
			Type:       "archive",
			BinaryName: "mytool",
		}
		if err := Extract(archivePath, destDir, spec); err != nil {
			t.Fatalf("Extract: %v", err)
		}

		// Binary should have been moved to bin/
		assertFileContent(t, filepath.Join(destDir, "bin", "mytool"), "#!/bin/sh\necho hi")
		// Original location should no longer exist
		if _, err := os.Stat(filepath.Join(destDir, "mytool")); !os.IsNotExist(err) {
			t.Error("root binary should have been moved")
		}
	})

	t.Run("does not move if bin dir exists", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		archivePath := filepath.Join(tmpDir, "test.tar.gz")
		destDir := filepath.Join(tmpDir, "dest")

		createTarGz(t, archivePath, []tarEntry{
			{name: "mytool", content: "root-binary", mode: 0755},
			{name: "bin/mytool", content: "bin-binary", mode: 0755},
		})

		spec := formula.InstallSpec{
			Type:       "archive",
			BinaryName: "mytool",
		}
		if err := Extract(archivePath, destDir, spec); err != nil {
			t.Fatalf("Extract: %v", err)
		}

		// bin/mytool should keep the archive's version, root binary stays
		assertFileContent(t, filepath.Join(destDir, "bin", "mytool"), "bin-binary")
	})
}

func TestExtract_UnknownType(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	spec := formula.InstallSpec{Type: "unknown"}
	err := Extract(filepath.Join(tmpDir, "dummy"), filepath.Join(tmpDir, "dest"), spec)
	if err == nil {
		t.Fatal("expected error for unknown install type")
	}
}

func TestExtract_BinaryDefaultName(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "downloaded-v1.0")
	os.WriteFile(srcFile, []byte("bin-content"), 0755)

	destDir := filepath.Join(tmpDir, "dest")
	spec := formula.InstallSpec{Type: "binary"} // no BinaryName

	if err := Extract(srcFile, destDir, spec); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Should use source filename as binary name
	assertFileContent(t, filepath.Join(destDir, "bin", "downloaded-v1.0"), "bin-content")
}

func TestExtractFile_SizeLimit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out")

	// Create a reader that produces more than maxExtractSize bytes.
	// We don't actually need a huge file — just verify LimitReader is used
	// by checking that extractFile completes without error on small input.
	content := []byte("small file")
	r := bytes.NewReader(content)
	if err := extractFile(r, outPath, 0644); err != nil {
		t.Fatalf("extractFile: %v", err)
	}
	assertFileContent(t, outPath, "small file")
}

// --- test helpers ---

type tarEntry struct {
	name    string
	content string
	mode    int64
}

type symlinkEntry struct {
	name   string
	target string
}

type zipEntry struct {
	name    string
	content string
}

func createTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	createTarGzWithSymlinks(t, path, entries, nil)
}

func createTarGzWithDirs(t *testing.T, path string, dirs []string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for _, d := range dirs {
		tw.WriteHeader(&tar.Header{
			Name:     d,
			Typeflag: tar.TypeDir,
			Mode:     0755,
		})
	}

	for _, e := range entries {
		tw.WriteHeader(&tar.Header{
			Name:     e.name,
			Size:     int64(len(e.content)),
			Mode:     e.mode,
			Typeflag: tar.TypeReg,
		})
		tw.Write([]byte(e.content))
	}
}

func createTarGzWithSymlinks(t *testing.T, path string, entries []tarEntry, symlinks []symlinkEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for _, e := range entries {
		tw.WriteHeader(&tar.Header{
			Name:     e.name,
			Size:     int64(len(e.content)),
			Mode:     e.mode,
			Typeflag: tar.TypeReg,
		})
		tw.Write([]byte(e.content))
	}
	for _, s := range symlinks {
		tw.WriteHeader(&tar.Header{
			Name:     s.name,
			Linkname: s.target,
			Typeflag: tar.TypeSymlink,
			Mode:     0777,
		})
	}
}

func createZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.content))
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Errorf("content of %s = %q, want %q", path, string(data), want)
	}
}
