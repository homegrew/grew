package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/config"
)

func init() {
	registerCaskChecks()
}

func registerCaskChecks() {
	caskChecks := []doctorCheck{
		{"check_cask_sandbox", "Check installed cask apps use App Sandbox", checkCaskSandbox},
		{"check_cask_notarization", "Check installed cask apps are notarized", checkCaskNotarization},
		{"check_cask_quarantine", "Check installed cask apps have quarantine attribute", checkCaskQuarantine},
	}
	registerExtraChecks(caskChecks)
}

// installedCaskApps returns the paths to .app bundles for all installed casks.
func installedCaskApps(ctx *doctorCtx) []string {
	paths := config.Default()
	cr := &cask.Caskroom{Path: paths.Caskroom}
	installed, err := cr.List()
	if err != nil || len(installed) == 0 {
		return nil
	}

	loader := newCaskLoader(paths.Taps)

	var apps []string
	for _, ic := range installed {
		c, err := loader.LoadByName(ic.Name)
		if err != nil {
			continue
		}
		for _, appName := range c.Artifacts.App {
			appPath := filepath.Join(paths.AppDir, appName)
			if _, err := os.Stat(appPath); err == nil {
				apps = append(apps, appPath)
			}
		}
	}
	return apps
}

func checkCaskSandbox(ctx *doctorCtx) {
	apps := installedCaskApps(ctx)
	for _, appPath := range apps {
		out, err := exec.Command("codesign", "-d", "--entitlements", "-", appPath).CombinedOutput()
		if err != nil {
			// Not signed at all — covered by notarization check.
			continue
		}
		if !strings.Contains(string(out), "com.apple.security.app-sandbox") {
			ctx.warn("cask app %s is not sandboxed (missing com.apple.security.app-sandbox entitlement)", filepath.Base(appPath))
		}
	}
}

func checkCaskNotarization(ctx *doctorCtx) {
	apps := installedCaskApps(ctx)
	for _, appPath := range apps {
		out, err := exec.Command("spctl", "--assess", "--type", "execute", "--verbose", appPath).CombinedOutput()
		if err != nil {
			combined := string(out)
			if strings.Contains(combined, "rejected") {
				ctx.warn("cask app %s is not notarized or fails Gatekeeper assessment", filepath.Base(appPath))
			} else if strings.Contains(combined, "a sealed resource is missing or invalid") {
				ctx.warn("cask app %s has an invalid code signature", filepath.Base(appPath))
			} else {
				ctx.warn("cask app %s: Gatekeeper check failed: %s", filepath.Base(appPath), strings.TrimSpace(combined))
			}
		}
	}
}

func checkCaskQuarantine(ctx *doctorCtx) {
	apps := installedCaskApps(ctx)
	for _, appPath := range apps {
		out, err := exec.Command("xattr", "-p", "com.apple.quarantine", appPath).CombinedOutput()
		if err != nil || len(strings.TrimSpace(string(out))) == 0 {
			ctx.warn("cask app %s is missing the quarantine attribute; macOS malware checks may have been bypassed",
				filepath.Base(appPath))
		} else {
			// Quarantine flag format: XXXX;TIMESTAMP;APPNAME;UUID
			// A flag starting with "00" means the user has already approved it.
			qVal := strings.TrimSpace(string(out))
			Debugf("    %s quarantine: %s\n", filepath.Base(appPath), qVal)
			parts := strings.SplitN(qVal, ";", 2)
			if len(parts) > 0 && len(parts[0]) >= 4 {
				flag := parts[0]
				// Flag "0083" or similar with bit 0x0040 means translocated; "00c1" etc.
				// We mostly just verify the attribute exists — the flag details are informational.
				Debugf("    quarantine flag: %s\n", flag)
			}
		}
	}
}

// applyCaskQuarantine sets the quarantine extended attribute on a .app path.
// Called during cask install to ensure macOS performs malware scanning.
func applyCaskQuarantine(appPath string) {
	qVal := fmt.Sprintf("0081;%d;grew;", quarantineEpoch())
	if err := exec.Command("xattr", "-w", "com.apple.quarantine", qVal, appPath).Run(); err != nil {
		Logf("    Note: could not set quarantine attribute on %s: %v\n", filepath.Base(appPath), err)
	}
}

func quarantineEpoch() int64 {
	info, err := os.Stat("/")
	if err != nil {
		return 0
	}
	return info.ModTime().Unix()
}
