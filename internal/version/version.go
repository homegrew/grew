package version

import (
	_ "embed"
	"strings"
)

//go:generate bash generate_version.sh
//go:embed version.txt
var version string

func SetVersion(v string) {
	if v == "" {
		version = "v0.0.0-UNKNOWN"
		return
	}
	version = v
}

// Version returns the version string.
func Version() string {
	return strings.TrimSpace(version)
}
