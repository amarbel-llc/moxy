// Package lifecyclelog writes proxy lifecycle events (child spawn/exit,
// JSON-RPC call tracing, moxin invocations, transport write errors) to a
// single append-only log at $XDG_LOG_HOME/moxy/lifecycle.log so events from
// multiple subsystems form one timeline.
package lifecyclelog

import (
	"log"
	"os"
	"path/filepath"
)

var logger *log.Logger

func init() {
	logHome := os.Getenv("XDG_LOG_HOME")
	if logHome == "" {
		home, _ := os.UserHomeDir()
		logHome = filepath.Join(home, ".local", "log")
	}
	logDir := filepath.Join(logHome, "moxy")
	os.MkdirAll(logDir, 0o755)
	f, err := os.OpenFile(
		filepath.Join(logDir, "lifecycle.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err == nil {
		logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	}
}

// Log writes a formatted line to lifecycle.log. No-op if the log file could
// not be opened at init time.
func Log(format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}
