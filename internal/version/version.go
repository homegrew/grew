package version

import (
	"strings"
)

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
