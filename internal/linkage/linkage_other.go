//go:build !darwin && !linux

package linkage

import "fmt"

func inspectDeps(_ string) ([]string, []string, error) {
	return nil, nil, fmt.Errorf("linkage checking not supported on this platform")
}

func isSystemLibPlatform(_ string) bool {
	return false
}
