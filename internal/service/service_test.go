package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homegrew/grew/internal/formula"
)

func TestStatus_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status Status
		want   string
	}{
		{StatusStopped, "stopped"},
		{StatusRunning, "running"},
		{StatusError, "error"},
		{StatusUnknown, "unknown"},
		{Status(99), "unknown"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestServiceLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"wget", "com.homegrew.wget"},
		{"hello-world", "com.homegrew.hello-world"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ServiceLabel(tt.name); got != tt.want {
				t.Errorf("ServiceLabel(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestResolveServiceCommand(t *testing.T) {
	t.Parallel()
	m := &Manager{
		CellarPath: "/home/user/.homegrew/Cellar",
		OptPath:    "/home/user/.homegrew/opt",
	}

	f := &formula.Formula{
		Name: "test",
		Service: &formula.ServiceSpec{
			Run: []string{"{prefix}/bin/test", "--config", "{opt}/test/config.yaml", "--data", "{cellar}/test/1.0.0/data"},
		},
	}

	want := []string{
		"/home/user/.homegrew/bin/test",
		"--config",
		"/home/user/.homegrew/opt/test/config.yaml",
		"--data",
		"/home/user/.homegrew/Cellar/test/1.0.0/data",
	}

	got := m.ResolveCommand(f)

	if len(got) != len(want) {
		t.Fatalf("got %d arguments, want %d", len(got), len(want))
	}

	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestManager_List(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := &Manager{
		ServiceDir: tmpDir,
	}

	ext := serviceFileExt()
	// Create some dummy service files
	files := []string{
		"com.homegrew.svc1" + ext,
		"com.homegrew.svc2" + ext,
		"other.file",
		"com.homegrew.dir" + ext, // This will be a directory
	}

	for _, name := range files {
		path := filepath.Join(tmpDir, name)
		if name == "com.homegrew.dir"+ext {
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(path, []byte("dummy"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}

	infos, err := m.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}

	// Should only find svc1 and svc2
	if len(infos) != 2 {
		t.Errorf("got %d services, want 2", len(infos))
	}

	foundSvc1 := false
	foundSvc2 := false
	for _, info := range infos {
		if info.Name == "svc1" {
			foundSvc1 = true
		}
		if info.Name == "svc2" {
			foundSvc2 = true
		}
	}

	if !foundSvc1 {
		t.Errorf("svc1 not found")
	}
	if !foundSvc2 {
		t.Errorf("svc2 not found")
	}
}

func TestManager_IsManaged(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := &Manager{
		ServiceDir: tmpDir,
	}

	ext := serviceFileExt()
	os.WriteFile(filepath.Join(tmpDir, "com.homegrew.managed"+ext), []byte("dummy"), 0644)

	if !m.IsManaged("managed") {
		t.Errorf("expected managed to be managed")
	}

	if m.IsManaged("unmanaged") {
		t.Errorf("expected unmanaged NOT to be managed")
	}

	if m.IsManaged("invalid/name") {
		t.Errorf("expected invalid/name NOT to be managed")
	}
}

func TestManager_Start_InvalidName(t *testing.T) {
	t.Parallel()
	m := &Manager{}
	f := &formula.Formula{Name: "invalid name!"}
	err := m.Start(f)
	if err == nil {
		t.Error("expected error for invalid formula name, got nil")
	}
}

func TestManager_Start_NoService(t *testing.T) {
	t.Parallel()
	m := &Manager{}
	f := &formula.Formula{Name: "valid"}
	err := m.Start(f)
	if err == nil {
		t.Error("expected error for formula with no service, got nil")
	}
}

func TestManager_Stop_InvalidName(t *testing.T) {
	t.Parallel()
	m := &Manager{}
	err := m.Stop("invalid name!")
	if err == nil {
		t.Error("expected error for invalid service name, got nil")
	}
}

func TestManager_Stop_NotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := &Manager{ServiceDir: tmpDir}
	err := m.Stop("notfound")
	if err == nil {
		t.Error("expected error for missing service file, got nil")
	}
}

func TestManager_Restart_InvalidName(t *testing.T) {
	t.Parallel()
	m := &Manager{}
	f := &formula.Formula{Name: "invalid name!"}
	err := m.Restart(f)
	if err == nil {
		t.Error("expected error for invalid formula name, got nil")
	}
}

func TestManager_WriteServiceFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	m := &Manager{
		ServiceDir: tmpDir,
		CellarPath: "/tmp/cellar",
		OptPath:    "/tmp/opt",
	}

	f := &formula.Formula{
		Name: "test-svc",
		Service: &formula.ServiceSpec{
			Run:        []string{"{prefix}/bin/test-svc"},
			WorkingDir: "/tmp/work",
			KeepAlive:  true,
			LogPath:    "/tmp/test.log",
		},
	}

	filePath := filepath.Join(tmpDir, serviceFileName(f.Name))
	err := m.writeServiceFile(f, filePath)
	if err != nil {
		t.Fatalf("writeServiceFile error: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	sContent := string(content)
	// Platform-specific checks
	if strings.Contains(sContent, "launchd") || strings.Contains(sContent, "plist") {
		// Darwin check
		if !strings.Contains(sContent, "<key>Label</key>") {
			t.Errorf("missing Label key in plist")
		}
		if !strings.Contains(sContent, "<string>com.homegrew.test-svc</string>") {
			t.Errorf("missing service label in plist")
		}
	} else if strings.Contains(sContent, "[Service]") {
		// Linux check
		if !strings.Contains(sContent, "ExecStart=") {
			t.Errorf("missing ExecStart in unit file")
		}
		if !strings.Contains(sContent, "test-svc") {
			t.Errorf("missing service name in unit file")
		}
	}
}
