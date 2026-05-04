package info

type InfoJSONv2 struct {
	Formulae []FormulaJSON `json:"formulae"`
	Casks    []CaskJSON    `json:"casks"`
}

type FormulaJSON struct {
	Name         string             `json:"name"`
	FullName     string             `json:"full_name"`
	Desc         string             `json:"desc"`
	License      string             `json:"license"`
	Homepage     string             `json:"homepage"`
	Versions     VersionsJSON       `json:"versions"`
	Dependencies []string           `json:"dependencies"`
	Installed    []InstalledPackageJSON `json:"installed"`
	KegOnly      bool               `json:"keg_only"`
}

type VersionsJSON struct {
	Stable string `json:"stable"`
}

type InstalledPackageJSON struct {
	Version         string `json:"version"`
	Linked          bool   `json:"linked"`
	BuiltFromSource bool   `json:"built_from_source"`
	InstalledAt     string `json:"installed_at"`
}

type CaskJSON struct {
	Token     string             `json:"token"`
	FullToken string             `json:"full_token"`
	Name      []string           `json:"name"`
	Desc      string             `json:"desc"`
	Homepage  string             `json:"homepage"`
	URL       string             `json:"url,omitempty"`
	Version   string             `json:"version"`
	Installed string             `json:"installed,omitempty"`
	Artifacts []CaskArtifactJSON `json:"artifacts,omitempty"`
}

type CaskArtifactJSON struct {
	App []string `json:"app,omitempty"`
	Bin []string `json:"bin,omitempty"`
}
