package version

import (
	"strconv"
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

// Compare compares two version strings (e.g., "v1.2.3").
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2.
// It handles versions with and without the 'v' prefix.
func Compare(v1, v2 string) int {
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	// Split by '.' and '-' to handle pre-releases like 1.0.0-rc1
	// For simplicity, we'll only compare the numeric parts first.
	p1 := strings.Split(strings.Split(v1, "-")[0], ".")
	p2 := strings.Split(strings.Split(v2, "-")[0], ".")

	for i := 0; i < len(p1) || i < len(p2); i++ {
		var n1, n2 int
		if i < len(p1) {
			n1, _ = strconv.Atoi(p1[i])
		}
		if i < len(p2) {
			n2, _ = strconv.Atoi(p2[i])
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	// If numeric parts are equal, check if one has a pre-release suffix.
	// In semver, 1.0.0-rc1 < 1.0.0.
	hasPre1 := strings.Contains(v1, "-")
	hasPre2 := strings.Contains(v2, "-")

	if hasPre1 && !hasPre2 {
		return -1
	}
	if !hasPre1 && hasPre2 {
		return 1
	}

	// If both have pre-releases, we do a simple string comparison (not fully semver compliant but close enough).
	if hasPre1 && hasPre2 {
		pre1 := strings.SplitN(v1, "-", 2)[1]
		pre2 := strings.SplitN(v2, "-", 2)[1]
		if pre1 < pre2 {
			return -1
		}
		if pre1 > pre2 {
			return 1
		}
	}

	return 0
}

// IsNewer returns true if v2 is newer than v1.
func IsNewer(v1, v2 string) bool {
	return Compare(v1, v2) == -1
}

