package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homegrew/grew/internal/cask"
	"github.com/homegrew/grew/internal/config"
)

// Helper to mock command execution if we need it
// For these tests, we can just create dummy app bundles and shell scripts acting as spctl/codesign/xattr
// by manipulating PATH to point to our mock binaries.

func setupMocks(t *testing.T) (string, func()) {
	tmpDir := t.TempDir()

	binDir := filepath.Join(tmpDir, "bin")
	os.MkdirAll(binDir, 0755)

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(filepath.ListSeparator)+origPath)

	return binDir, func() {
		// Nothing to do, t.Setenv handles cleanup
	}
}

func writeMockCmd(binDir, cmdName, scriptContent string) {
	cmdPath := filepath.Join(binDir, cmdName)
	os.WriteFile(cmdPath, []byte(scriptContent), 0755)
}

func TestInstalledCaskApps(t *testing.T) {
	tmpDir := t.TempDir()
	caskroom := filepath.Join(tmpDir, "Caskroom")
	appDir := filepath.Join(tmpDir, "Applications")

	os.MkdirAll(caskroom, 0755)
	os.MkdirAll(appDir, 0755)

	// Create installed cask directory with a valid version
	os.MkdirAll(filepath.Join(caskroom, "mycask", "1.0.0"), 0755)
	
	// Create the actual app bundle
	appPath := filepath.Join(appDir, "MyApp.app")
	os.MkdirAll(appPath, 0755)

	ctx := &Context{
		Paths: config.FromRoot(tmpDir, appDir, filepath.Join(tmpDir, "Cache")),
		Casks: []*cask.Cask{
			{
				Name: "mycask",
				Artifacts: cask.Artifacts{
					App: []string{"MyApp.app"},
				},
			},
			{
				Name: "notinstalled",
				Artifacts: cask.Artifacts{
					App: []string{"Other.app"},
				},
			},
			{
				Name: "mycask", // installed but app missing in appDir
				Artifacts: cask.Artifacts{
					App: []string{"Missing.app"},
				},
			},
			{
				Name: "mycask", // path traversal attempt
				Artifacts: cask.Artifacts{
					App: []string{"../Escaping.app"},
				},
			},
		},
	}

	apps := installedCaskApps(ctx)

	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}
	if apps[0] != appPath {
		t.Errorf("expected app path %s, got %s", appPath, apps[0])
	}
}

func TestCheckCaskSandbox(t *testing.T) {
	t.Run("sandboxed", func(t *testing.T) {
		binDir, _ := setupMocks(t)

		writeMockCmd(binDir, "codesign", "#!/bin/sh\necho 'com.apple.security.app-sandbox'\n")

		tmpDir := t.TempDir()
		appDir := filepath.Join(tmpDir, "Applications")
		os.MkdirAll(appDir, 0755)
		appPath := filepath.Join(appDir, "Test.app")
		os.MkdirAll(appPath, 0755)
		
		caskroom := filepath.Join(tmpDir, "Caskroom")
		os.MkdirAll(filepath.Join(caskroom, "testcask", "1.0.0"), 0755)
		
		ctx := &Context{
			Paths: config.FromRoot(tmpDir, appDir, filepath.Join(tmpDir, "Cache")),
			Casks: []*cask.Cask{
				{Name: "testcask", Artifacts: cask.Artifacts{App: []string{"Test.app"}}},
			},
		}

		assertDoctorWarnings(t, ctx, CheckCaskSandbox, "")
	})

	t.Run("not sandboxed", func(t *testing.T) {
		binDir, _ := setupMocks(t)

		writeMockCmd(binDir, "codesign", "#!/bin/sh\necho 'some other entitlement'\n")

		tmpDir := t.TempDir()
		appDir := filepath.Join(tmpDir, "Applications")
		os.MkdirAll(appDir, 0755)
		appPath := filepath.Join(appDir, "Test.app")
		os.MkdirAll(appPath, 0755)
		
		caskroom := filepath.Join(tmpDir, "Caskroom")
		os.MkdirAll(filepath.Join(caskroom, "testcask", "1.0.0"), 0755)
		
		ctx := &Context{
			Paths: config.FromRoot(tmpDir, appDir, filepath.Join(tmpDir, "Cache")),
			Casks: []*cask.Cask{
				{Name: "testcask", Artifacts: cask.Artifacts{App: []string{"Test.app"}}},
			},
		}

		assertDoctorWarnings(t, ctx, CheckCaskSandbox, "is not sandboxed")
	})
	
	t.Run("codesign fails", func(t *testing.T) {
		binDir, _ := setupMocks(t)

		writeMockCmd(binDir, "codesign", "#!/bin/sh\nexit 1\n")

		tmpDir := t.TempDir()
		appDir := filepath.Join(tmpDir, "Applications")
		os.MkdirAll(appDir, 0755)
		appPath := filepath.Join(appDir, "Test.app")
		os.MkdirAll(appPath, 0755)
		
		caskroom := filepath.Join(tmpDir, "Caskroom")
		os.MkdirAll(filepath.Join(caskroom, "testcask", "1.0.0"), 0755)
		
		ctx := &Context{
			Paths: config.FromRoot(tmpDir, appDir, filepath.Join(tmpDir, "Cache")),
			Casks: []*cask.Cask{
				{Name: "testcask", Artifacts: cask.Artifacts{App: []string{"Test.app"}}},
			},
		}

		assertDoctorWarnings(t, ctx, CheckCaskSandbox, "") // Ignored if codesign fails
	})
}

func TestCheckCaskNotarization(t *testing.T) {
	tests := []struct{
		name string
		script string
		wantWarn string
	}{
		{"notarized", "#!/bin/sh\nexit 0\n", ""},
		{"rejected", "#!/bin/sh\necho 'rejected'\nexit 1\n", "is not notarized or fails Gatekeeper assessment"},
		{"invalid sig", "#!/bin/sh\necho 'a sealed resource is missing or invalid'\nexit 1\n", "has an invalid code signature"},
		{"other error", "#!/bin/sh\necho 'some error'\nexit 1\n", "Gatekeeper check failed:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir, _ := setupMocks(t)

			writeMockCmd(binDir, "spctl", tt.script)

			tmpDir := t.TempDir()
			appDir := filepath.Join(tmpDir, "Applications")
			os.MkdirAll(appDir, 0755)
			appPath := filepath.Join(appDir, "Test.app")
			os.MkdirAll(appPath, 0755)
			
			caskroom := filepath.Join(tmpDir, "Caskroom")
			os.MkdirAll(filepath.Join(caskroom, "testcask", "1.0.0"), 0755)
			
			ctx := &Context{
				Paths: config.FromRoot(tmpDir, appDir, filepath.Join(tmpDir, "Cache")),
				Casks: []*cask.Cask{
					{Name: "testcask", Artifacts: cask.Artifacts{App: []string{"Test.app"}}},
				},
			}

			assertDoctorWarnings(t, ctx, CheckCaskNotarization, tt.wantWarn)
		})
	}
}

func TestCheckCaskQuarantine(t *testing.T) {
	tests := []struct{
		name string
		script string
		wantWarn string
	}{
		{"quarantined", "#!/bin/sh\necho '0081;5f;App;UUID'\nexit 0\n", ""},
		{"not quarantined (empty)", "#!/bin/sh\necho ''\nexit 0\n", "is missing the quarantine attribute"},
		{"not quarantined (error)", "#!/bin/sh\nexit 1\n", "is missing the quarantine attribute"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir, _ := setupMocks(t)

			writeMockCmd(binDir, "xattr", tt.script)

			tmpDir := t.TempDir()
			appDir := filepath.Join(tmpDir, "Applications")
			os.MkdirAll(appDir, 0755)
			appPath := filepath.Join(appDir, "Test.app")
			os.MkdirAll(appPath, 0755)
			
			caskroom := filepath.Join(tmpDir, "Caskroom")
			os.MkdirAll(filepath.Join(caskroom, "testcask", "1.0.0"), 0755)
			
			ctx := &Context{
				Paths: config.FromRoot(tmpDir, appDir, filepath.Join(tmpDir, "Cache")),
				Casks: []*cask.Cask{
					{Name: "testcask", Artifacts: cask.Artifacts{App: []string{"Test.app"}}},
				},
			}

			assertDoctorWarnings(t, ctx, CheckCaskQuarantine, tt.wantWarn)
		})
	}
}

func TestRegisterCaskChecks(t *testing.T) {
	// Count extra checks before
	numBefore := len(ExtraChecks)

	RegisterCaskChecks()

	if len(ExtraChecks) <= numBefore {
		t.Errorf("Expected RegisterCaskChecks to add checks to ExtraChecks")
	}

	var foundSandbox, foundNotarization, foundQuarantine bool
	for _, c := range ExtraChecks {
		switch c.Name {
		case "check_cask_sandbox":
			foundSandbox = true
		case "check_cask_notarization":
			foundNotarization = true
		case "check_cask_quarantine":
			foundQuarantine = true
		}
	}

	if !foundSandbox || !foundNotarization || !foundQuarantine {
		t.Errorf("Not all cask checks registered in ExtraChecks. sandbox: %v, notarization: %v, quarantine: %v", foundSandbox, foundNotarization, foundQuarantine)
	}
}
