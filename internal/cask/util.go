package cask

import (
	"fmt"
	"strings"
)

func (l *Loader) Search(query string) []string {
	var results []string
	casks, err := l.LoadAll()
	if err != nil {
		return nil
	}
	for _, c := range casks {
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(c.Description), strings.ToLower(query)) {
			results = append(results, c.Name)
		}
	}
	return results
}

func (cr *Caskroom) ListInstalled() ([]InstalledCask, error) {
	return cr.List()
}

func LoadInfoData(loader *Loader, cr *Caskroom, name string) (*Cask, string, error) {
	c, err := loader.LoadByName(name)
	if err != nil {
		return nil, "", err
	}
	ver := ""
	if cr.IsInstalled(c.Name) {
		var errVer error
		ver, errVer = cr.InstalledVersion(c.Name)
		if errVer != nil {
			return nil, "", fmt.Errorf("read installed version for %q: %w", c.Name, errVer)
		}
	}
	return c, ver, nil
}

func PrintInfo(loader *Loader, cr *Caskroom, name string) error {
	c, ver, err := LoadInfoData(loader, cr, name)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %s %s (cask)\n", c.Name, c.Description, c.Version)
	if c.Tap != "" {
		fmt.Printf("From: %s\n", c.Tap)
	}
	fmt.Printf("Homepage: %s\n", c.Homepage)
	fmt.Printf("License:  %s\n", c.License)

	if ver != "" {
		fmt.Printf("Installed: %s\n", ver)
	} else {
		fmt.Println("Installed: no")
	}

	if len(c.Artifacts.App) > 0 {
		fmt.Printf("Apps: %s\n", strings.Join(c.Artifacts.App, ", "))
	}
	if len(c.Artifacts.Bin) > 0 {
		fmt.Printf("Binaries: %s\n", strings.Join(c.Artifacts.Bin, ", "))
	}

	platforms := make([]string, 0, len(c.URL))
	for k := range c.URL {
		platforms = append(platforms, k)
	}
	fmt.Printf("Platforms: %s\n", strings.Join(platforms, ", "))

	if c.Caveats != "" {
		fmt.Println("\nCaveats:")
		fmt.Println(c.Caveats)
	}

	return nil
}
