package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homegrew/grew/internal/formula"
)

// Status represents the state of a service.
type Status int

const (
	StatusStopped Status = iota
	StatusRunning
	StatusError
	StatusUnknown
)

func (s Status) String() string {
	switch s {
	case StatusStopped:
		return "stopped"
	case StatusRunning:
		return "running"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// Info holds runtime information about a managed service.
type Info struct {
	Name    string
	Status  Status
	PID     int    // 0 if not running
	File    string // path to the generated service file (plist/unit)
	LogPath string
}

// Manager manages formula services using the platform init system.
type Manager struct {
	ServiceDir string // directory where service files are written
	CellarPath string
	OptPath    string
	Loader     *formula.Loader
}

// ServiceLabel returns the reverse-dns label used for a formula service.
func ServiceLabel(name string) string {
	return "com.homegrew." + name
}

// serviceFileName returns the file name for a service definition.
func serviceFileName(name string) string {
	return ServiceLabel(name) + serviceFileExt()
}

// List returns info for all managed services.
func (m *Manager) List() ([]Info, error) {
	entries, err := os.ReadDir(m.ServiceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read service dir: %w", err)
	}

	var infos []Info
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		label := strings.TrimSuffix(e.Name(), serviceFileExt())
		if !strings.HasPrefix(label, "com.homegrew.") {
			continue
		}
		name := strings.TrimPrefix(label, "com.homegrew.")
		filePath := filepath.Join(m.ServiceDir, e.Name())
		status, pid := m.serviceStatus(name)
		infos = append(infos, Info{
			Name:   name,
			Status: status,
			PID:    pid,
			File:   filePath,
		})
	}
	return infos, nil
}

// Start installs and starts the service for the given formula.
func (m *Manager) Start(f *formula.Formula) error {
	if f.Service == nil {
		return fmt.Errorf("formula %q does not define a service", f.Name)
	}
	if err := os.MkdirAll(m.ServiceDir, 0755); err != nil {
		return fmt.Errorf("create service dir: %w", err)
	}

	filePath := filepath.Join(m.ServiceDir, serviceFileName(f.Name))
	if err := m.writeServiceFile(f, filePath); err != nil {
		return err
	}

	return m.loadService(f.Name, filePath)
}

// Stop stops and unloads the service for the given formula.
func (m *Manager) Stop(name string) error {
	filePath := filepath.Join(m.ServiceDir, serviceFileName(name))
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("no service file found for %q", name)
	}
	if err := m.unloadService(name, filePath); err != nil {
		return err
	}
	return os.Remove(filePath)
}

// Restart stops then starts the service.
func (m *Manager) Restart(f *formula.Formula) error {
	filePath := filepath.Join(m.ServiceDir, serviceFileName(f.Name))
	if _, err := os.Stat(filePath); err == nil {
		// Best-effort stop; ignore errors if not currently loaded.
		_ = m.unloadService(f.Name, filePath)
		os.Remove(filePath)
	}
	return m.Start(f)
}

// IsManaged returns true if a service file exists for this formula.
func (m *Manager) IsManaged(name string) bool {
	filePath := filepath.Join(m.ServiceDir, serviceFileName(name))
	_, err := os.Stat(filePath)
	return err == nil
}

// ResolveCommand expands the run command, substituting {prefix}, {opt}, {cellar}.
// Exported so the CLI can use it for the `run` subcommand.
func (m *Manager) ResolveCommand(f *formula.Formula) []string {
	return m.resolveServiceCommand(f)
}

// resolveServiceCommand expands the run command, substituting {prefix}, {opt}, {cellar}.
func (m *Manager) resolveServiceCommand(f *formula.Formula) []string {
	cmd := make([]string, len(f.Service.Run))
	for i, arg := range f.Service.Run {
		arg = strings.ReplaceAll(arg, "{prefix}", filepath.Dir(m.CellarPath))
		arg = strings.ReplaceAll(arg, "{opt}", m.OptPath)
		arg = strings.ReplaceAll(arg, "{cellar}", m.CellarPath)
		cmd[i] = arg
	}
	return cmd
}
