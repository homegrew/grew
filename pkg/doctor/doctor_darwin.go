
package doctor

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/pkg/config"
)

func init() {
	RegisterCaskChecks()
}

// RegisterCaskChecks appends Darwin-specific cask security checks to the global diagnostic list.
func RegisterCaskChecks() {
	caskChecks := []Check{
		{"check_cask_sandbox", "Check installed cask apps use App Sandbox", CheckCaskSandbox},
		{"check_cask_notarization", "Check installed cask apps are notarized", CheckCaskNotarization},
		{"check_cask_quarantine", "Check installed cask apps have quarantine attribute", CheckCaskQuarantine},
	}
	RegisterExtraChecks(caskChecks)
}

// installedCaskApps returns the paths to .app bundles for all installed casks.
func installedCaskApps(ctx *Context) []string {
	paths := config.Default()
	if ctx != nil {
		paths = ctx.Paths
	}

	cr := &cask.Caskroom{Path: paths.Caskroom}
	installed, err := cr.List()
	if err != nil || len(installed) == 0 {
		return nil
	}

	var apps []string
	for _, c := range ctx.Casks {
		// We only want to check INSTALLED casks.
		if !cr.IsInstalled(c.Name) {
			continue
		}
		for _, appName := range c.Artifacts.App {
			if filepath.Base(appName) != appName {
				continue // reject path traversal in artifact names
			}
			appPath := filepath.Join(paths.AppDir, appName)
			if _, err := os.Stat(appPath); err == nil {
				apps = append(apps, appPath)
			}
		}
	}
	return apps
}

// CheckCaskSandbox verifies that installed cask applications have the App Sandbox entitlement.
func CheckCaskSandbox(ctx *Context) {
	apps := installedCaskApps(ctx)
	for _, appPath := range apps {
		out, err := exec.Command("codesign", "-d", "--entitlements", "-", "--", appPath).CombinedOutput()
		if err != nil {
			// Not signed at all — covered by notarization check.
			continue
		}
		if !strings.Contains(string(out), "com.apple.security.app-sandbox") {
			ctx.Warn("cask app %s is not sandboxed (missing com.apple.security.app-sandbox entitlement)", filepath.Base(appPath))
		}
	}
}

// CheckCaskNotarization verifies that installed cask applications are notarized and pass Gatekeeper assessment.
func CheckCaskNotarization(ctx *Context) {
	apps := installedCaskApps(ctx)
	for _, appPath := range apps {
		out, err := exec.Command("spctl", "--assess", "--type", "execute", "--verbose", "--", appPath).CombinedOutput()
		if err != nil {
			combined := string(out)
			if strings.Contains(combined, "rejected") {
				ctx.Warn("cask app %s is not notarized or fails Gatekeeper assessment", filepath.Base(appPath))
			} else if strings.Contains(combined, "a sealed resource is missing or invalid") {
				ctx.Warn("cask app %s has an invalid code signature", filepath.Base(appPath))
			} else {
				ctx.Warn("cask app %s: Gatekeeper check failed: %s", filepath.Base(appPath), strings.TrimSpace(combined))
			}
		}
	}
}

// CheckCaskQuarantine verifies that installed cask applications have the macOS quarantine attribute set.
func CheckCaskQuarantine(ctx *Context) {
	apps := installedCaskApps(ctx)
	for _, appPath := range apps {
		out, err := exec.Command("xattr", "-p", "com.apple.quarantine", "--", appPath).CombinedOutput()
		if err != nil || len(strings.TrimSpace(string(out))) == 0 {
			ctx.Warn("cask app %s is missing the quarantine attribute; macOS malware checks may have been bypassed",
				filepath.Base(appPath))
		} else {
			// Quarantine flag format: XXXX;TIMESTAMP;APPNAME;UUID
			// A flag starting with "00" means the user has already approved it.
			qVal := strings.TrimSpace(string(out))
			slog.Debug(fmt.Sprintf("%s quarantine: %s", filepath.Base(appPath), qVal))
			parts := strings.SplitN(qVal, ";", 2)
			if len(parts) > 0 && len(parts[0]) >= 4 {
				flag := parts[0]
				slog.Debug("quarantine flag: " + flag)
			}
		}
	}
}
