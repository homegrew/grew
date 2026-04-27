//go:build darwin

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// applyCaskQuarantine sets the quarantine extended attribute on a .app path.
// Called during cask install to ensure macOS performs malware scanning.
// Returns an error if the attribute cannot be set — callers should refuse
// to install unless --no-quarantine is explicitly passed.
func applyCaskQuarantine(appPath string) error {
	qVal := fmt.Sprintf("0081;%d;grew;", quarantineEpoch())
	if err := exec.Command("xattr", "-w", "com.apple.quarantine", qVal, "--", appPath).Run(); err != nil {
		return fmt.Errorf("set quarantine on %s: %w\n"+
			"  Without quarantine, macOS will not scan this app for malware.\n"+
			"  Use --no-quarantine to install anyway (not recommended).", filepath.Base(appPath), err)
	}
	return nil
}

func quarantineEpoch() int64 {
	info, err := os.Stat("/")
	if err != nil {
		return 0
	}
	return info.ModTime().Unix()
}
