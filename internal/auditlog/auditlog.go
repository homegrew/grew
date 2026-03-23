// Code for full auditlog implementation

package auditlog

import (
	"path/filepath"
)

// AuditLog represents the audit log.
type AuditLog struct {
	logDir string
}

// New initializes a new AuditLog
func New(logDir string) *AuditLog {
	if logDir == "" {
		logDir = config.Default().Log
	}

	// Normalize logDir
	logDir, _ = filepath.Abs(logDir)
	logDir = filepath.Clean(logDir)

	return &AuditLog{logDir: logDir}
}

// Additional methods for AuditLog...
