package version

import (
	"strings"
)

var version string

func SetVersion(v string) {
	if v == "" {
		version = "v0.0.0-UNKNOWN"

	} else if !strings.HasPrefix(v, "v") {
		version = "v" + v
	} else {
		version = v
	}

	version = strings.TrimSpace(version)
}

// Version returns the version string.
func Version() string {
	return version
}
