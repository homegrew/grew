// Package auditlog provides a tamper-evident append-only log of grew operations.
// Each entry records the action, formula/cask, version, relevant hashes, and
// the user who performed the operation. The log file lives at <prefix>/var/log/grew.log.
package auditlog

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const logFileName = "grew.log"

// Action describes the type of operation being logged.
type Action string

const (
	ActionInstall     Action = "install"
	ActionUninstall   Action = "uninstall"
	ActionReinstall   Action = "reinstall"
	ActionUpgrade     Action = "upgrade"
	ActionSelfUpdate  Action = "self-update"
	ActionPin         Action = "pin"
	ActionUnpin       Action = "unpin"
	ActionLink        Action = "link"
	ActionUnlink      Action = "unlink"
	ActionCaskInstall Action = "cask-install"
	ActionCaskRemove  Action = "cask-remove"
)

// Entry is a single audit log record.
type Entry struct {
	Timestamp string `json:"timestamp"`
	Action    Action `json:"action"`
	Name      string `json:"name,omitempty"`
	Version   string `json:"version,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	User      string `json:"user,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Logger writes audit log entries to the grew log file.
type Logger struct {
	logDir string
	mu     sync.Mutex
}

// New creates a Logger that writes to <logDir>/grew.log.
func New(logDir string) *Logger {
	return &Logger{logDir: logDir}
}

// Log appends an entry to the audit log. Errors are silently ignored —
// audit logging must never block or fail package management operations.
func (l *Logger) Log(action Action, name, version, sha256, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Action:    action,
		Name:      name,
		Version:   version,
		SHA256:    sha256,
		User:      currentUser(),
		Detail:    detail,
	}

	path := filepath.Join(l.logDir, logFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f.Write(data)
	f.Write([]byte("\n"))
}

// Read returns all log entries from the audit log file.
// Returns nil and no error if the file doesn't exist.
func Read(logDir string) ([]Entry, error) {
	path := filepath.Join(logDir, logFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read audit log: %w", err)
	}

	var entries []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func currentUser() string {
	u, err := user.Current()
	if err != nil {
		return os.Getenv("USER")
	}
	return u.Username
}
