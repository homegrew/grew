package quarantine

import (
	"fmt"
	"strings"
)

// Apply sets the macOS quarantine attribute on a given path.
// It requires the download URL and the origin URL.
func Apply(appPath, downloadURL, originURL string) error {
	_, err := RunScript(QuarantineScript, appPath, downloadURL, originURL)
	return err
}

// Trash moves the given paths to the macOS Trash.
// Returns a list of paths successfully trashed, and an error if any failed.
func Trash(paths ...string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out, err := RunScript(TrashScript, paths...)
	
	lines := strings.Split(out, "\n")
	var trashed []string
	if len(lines) > 0 && lines[0] != "" {
		trashed = strings.Split(lines[0], ":")
	}

	if err != nil {
		var untrashable []string
		if len(lines) > 1 && lines[1] != "" {
			untrashable = strings.Split(lines[1], ":")
		}
		return trashed, fmt.Errorf("failed to trash items: %v (untrashable: %v)", err, untrashable)
	}

	return trashed, nil
}
