package tap

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Manager struct {
	TapsDir    string
	EmbeddedFS fs.FS
}

func (m *Manager) InitCore() error {
	coreTapDir := filepath.Join(m.TapsDir, "core")
	if err := os.MkdirAll(coreTapDir, 0755); err != nil {
		return fmt.Errorf("create core tap dir: %w", err)
	}
	return fs.WalkDir(m.EmbeddedFS, "taps/core", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(m.EmbeddedFS, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		destFile := filepath.Join(coreTapDir, d.Name())
		if _, statErr := os.Stat(destFile); statErr == nil {
			return nil // already exists
		}
		return atomicWrite(destFile, data, 0644)
	})
}

func (m *Manager) InitCask() error {
	caskTapDir := filepath.Join(m.TapsDir, "cask")
	if err := os.MkdirAll(caskTapDir, 0755); err != nil {
		return fmt.Errorf("create cask tap dir: %w", err)
	}
	return fs.WalkDir(m.EmbeddedFS, "taps/cask", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(m.EmbeddedFS, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		destFile := filepath.Join(caskTapDir, d.Name())
		if _, statErr := os.Stat(destFile); statErr == nil {
			return nil
		}
		return atomicWrite(destFile, data, 0644)
	})
}

func (m *Manager) Update() (int, error) {
	count := 0
	for _, sub := range []string{"core", "cask"} {
		tapDir := filepath.Join(m.TapsDir, sub)
		if err := os.MkdirAll(tapDir, 0755); err != nil {
			return count, fmt.Errorf("create %s tap dir: %w", sub, err)
		}
		err := fs.WalkDir(m.EmbeddedFS, "taps/"+sub, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(m.EmbeddedFS, path)
			if err != nil {
				return fmt.Errorf("read embedded %s: %w", path, err)
			}
			destFile := filepath.Join(tapDir, d.Name())
			if err := atomicWrite(destFile, data, 0644); err != nil {
				return fmt.Errorf("write %s: %w", destFile, err)
			}
			count++
			return nil
		})
		if err != nil {
			return count, err
		}
	}
	return count, nil
}

// atomicWrite writes data to a temp file in the same directory, then renames
// it to the target path. This prevents partial writes from corrupting files.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".grew-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
