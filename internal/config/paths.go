package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	Root     string
	Cellar   string
	Opt      string
	Bin      string
	Lib      string
	Include  string
	Taps     string
	CoreTap  string
	CaskTap  string
	Caskroom string
	AppDir   string
	Tmp      string
}

func Default() Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	root := os.Getenv("HOMEGREW_PREFIX")
	if root == "" {
		root = filepath.Join(home, ".grew")
	}
	appDir := os.Getenv("HOMEGREW_APPDIR")
	if appDir == "" {
		appDir = filepath.Join(home, "Applications")
	}

	return Paths{
		Root:     root,
		Cellar:   filepath.Join(root, "Cellar"),
		Opt:      filepath.Join(root, "opt"),
		Bin:      filepath.Join(root, "bin"),
		Lib:      filepath.Join(root, "lib"),
		Include:  filepath.Join(root, "include"),
		Taps:     filepath.Join(root, "Taps"),
		CoreTap:  filepath.Join(root, "Taps", "core"),
		CaskTap:  filepath.Join(root, "Taps", "cask"),
		Caskroom: filepath.Join(root, "Caskroom"),
		AppDir:   appDir,
		Tmp:      filepath.Join(root, "tmp"),
	}
}

func (p Paths) Init() error {
	dirs := []string{
		p.Root, p.Cellar, p.Opt, p.Bin, p.Lib,
		p.Include, p.Taps, p.CoreTap, p.CaskTap,
		p.Caskroom, p.AppDir, p.Tmp,
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", d, err)
		}
	}
	return nil
}
