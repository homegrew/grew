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

func serviceFileExt() string { return ".service" }

func defaultServiceDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

// DefaultManager returns a Manager configured for systemd --user.
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

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=grew service for {{.Name}}

[Service]
ExecStart={{.ExecStart}}
{{- if .WorkingDir}}
WorkingDirectory={{.WorkingDir}}
{{- end}}
{{- if eq .Type "simple"}}
Type=simple
{{- end}}
Restart={{.Restart}}
{{- if .LogPath}}
StandardOutput=append:{{.LogPath}}
{{- end}}
{{- if .ErrorLogPath}}
StandardError=append:{{.ErrorLogPath}}
{{- end}}

[Install]
WantedBy=default.target
`))

type unitData struct {
	Name         string
	ExecStart    string
	WorkingDir   string
	Type         string
	Restart      string
	LogPath      string
	ErrorLogPath string
}

func (m *Manager) writeServiceFile(f *formula.Formula, path string) error {
	cmd := m.resolveServiceCommand(f)
	if len(cmd) == 0 {
		return fmt.Errorf("formula %q service has no run command", f.Name)
	}

	restart := "no"
	if f.Service.KeepAlive {
		restart = "always"
	}

	svcType := "simple"
	if f.Service.RunType != "" {
		svcType = f.Service.RunType
	}

	data := unitData{
		Name:         f.Name,
		ExecStart:    strings.Join(cmd, " "),
		WorkingDir:   f.Service.WorkingDir,
		Type:         svcType,
		Restart:      restart,
		LogPath:      f.Service.LogPath,
		ErrorLogPath: f.Service.ErrorLogPath,
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create unit file: %w", err)
	}
	defer file.Close()

	if err := unitTmpl.Execute(file, data); err != nil {
		os.Remove(path)
		return fmt.Errorf("write unit file: %w", err)
	}
	return nil
}

func (m *Manager) loadService(name, filePath string) error {
	unitName := ServiceLabel(name) + ".service"
	// Reload so systemd picks up the new file.
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s (%w)", out, err)
	}
	cmd := exec.Command("systemctl", "--user", "enable", "--now", unitName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl enable --now %s: %w", name, err)
	}
	return nil
}

func (m *Manager) unloadService(name, filePath string) error {
	unitName := ServiceLabel(name) + ".service"
	cmd := exec.Command("systemctl", "--user", "disable", "--now", unitName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl disable --now %s: %w", name, err)
	}
	return nil
}

func (m *Manager) serviceStatus(name string) (Status, int) {
	unitName := ServiceLabel(name) + ".service"
	out, err := exec.Command("systemctl", "--user", "show", unitName,
		"--property=ActiveState,MainPID").CombinedOutput()
	if err != nil {
		return StatusUnknown, 0
	}

	var state string
	var pid int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ActiveState=") {
			state = strings.TrimPrefix(line, "ActiveState=")
		}
		if strings.HasPrefix(line, "MainPID=") {
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, "MainPID="))
		}
	}

	switch state {
	case "active":
		return StatusRunning, pid
	case "failed":
		return StatusError, 0
	case "inactive":
		return StatusStopped, 0
	default:
		return StatusStopped, 0
	}
}
