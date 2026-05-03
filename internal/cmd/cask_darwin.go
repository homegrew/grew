
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/homegrew/grew/internal/quarantine"
)

// applyCaskQuarantine sets the quarantine extended attribute on a .app path.
// Called during cask install to ensure macOS performs malware scanning.
// Returns an error if the attribute cannot be set — callers should refuse
// to install unless --no-quarantine is explicitly passed.
func applyCaskQuarantine(appPath, dlURL string) error {
	if err := quarantine.Apply(appPath, dlURL, dlURL); err != nil {
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
