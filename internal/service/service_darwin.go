package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/homegrew/grew/internal/formula"
)

func serviceFileExt() string { return ".plist" }

func defaultServiceDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// DefaultManager returns a Manager configured for macOS launchd.
func DefaultManager(cellarPath, optPath string, loader *formula.Loader) (*Manager, error) {
	dir, err := defaultServiceDir()
	if err != nil {
		return nil, err
	}
	return &Manager{
		ServiceDir: dir,
		CellarPath: cellarPath,
		OptPath:    optPath,
		Loader:     loader,
	}, nil
}

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
{{- range .Command}}
		<string>{{.}}</string>
{{- end}}
	</array>
{{- if .WorkingDir}}
	<key>WorkingDirectory</key>
	<string>{{.WorkingDir}}</string>
{{- end}}
{{- if .KeepAlive}}
	<key>KeepAlive</key>
	<true/>
{{- end}}
	<key>RunAtLoad</key>
	<true/>
{{- if .LogPath}}
	<key>StandardOutPath</key>
	<string>{{.LogPath}}</string>
{{- end}}
{{- if .ErrorLogPath}}
	<key>StandardErrorPath</key>
	<string>{{.ErrorLogPath}}</string>
{{- end}}
</dict>
</plist>
`))

type plistData struct {
	Label        string
	Command      []string
	WorkingDir   string
	KeepAlive    bool
	LogPath      string
	ErrorLogPath string
}

func (m *Manager) writeServiceFile(f *formula.Formula, path string) error {
	cmd := m.resolveServiceCommand(f)
	if len(cmd) == 0 {
		return fmt.Errorf("formula %q service has no run command", f.Name)
	}

	data := plistData{
		Label:        ServiceLabel(f.Name),
		Command:      cmd,
		WorkingDir:   f.Service.WorkingDir,
		KeepAlive:    f.Service.KeepAlive,
		LogPath:      f.Service.LogPath,
		ErrorLogPath: f.Service.ErrorLogPath,
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}
	defer file.Close()

	if err := plistTmpl.Execute(file, data); err != nil {
		os.Remove(path)
		return fmt.Errorf("write plist: %w", err)
	}
	return nil
}

func (m *Manager) loadService(name, filePath string) error {
	cmd := exec.Command("launchctl", "load", "-w", filePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl load %s: %w", name, err)
	}
	return nil
}

func (m *Manager) unloadService(name, filePath string) error {
	cmd := exec.Command("launchctl", "unload", "-w", filePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl unload %s: %w", name, err)
	}
	return nil
}

func (m *Manager) serviceStatus(name string) (Status, int) {
	label := ServiceLabel(name)
	out, err := exec.Command("launchctl", "list", label).CombinedOutput()
	if err != nil {
		return StatusStopped, 0
	}
	// Parse the launchctl list <label> output for PID.
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.Contains(line, "PID") {
			// The "PID" line in launchctl list <label> looks like:
			//   "PID" = 12345;
			for i, f := range fields {
				if f == "\"PID\"" && i+2 < len(fields) {
					pidStr := strings.TrimSuffix(fields[i+2], ";")
					if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
						return StatusRunning, pid
					}
				}
			}
		}
	}
	// launchctl list succeeded but we couldn't parse PID — it's loaded but may not have a running process.
	return StatusRunning, 0
}
