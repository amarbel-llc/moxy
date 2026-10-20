// Package diaglog is the one home of moxy's diagnostic log files
// (<log home>/moxy/<name>.log: debug, moxin.<pid>, lifecycle, hook). It is
// off until Enable is called, so a process that embeds moxy as a library
// (FDR 0014) writes no files unless it asks to; the moxy binary enables it
// first thing in main, which keeps every subcommand's logging exactly as it
// was when each package opened its own file at init.
package diaglog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	mu      sync.Mutex
	dir     string // "" until Enable; the directory the log files live in
	loggers = map[string]*log.Logger{}
)

// DefaultLogHome is where the binary keeps its logs: $XDG_LOG_HOME, else
// ~/.local/log. The moxy/ subdirectory is added by Enable.
func DefaultLogHome() string {
	if logHome := os.Getenv("XDG_LOG_HOME"); logHome != "" {
		return logHome
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "log")
}

// Enable turns the diagnostic logs on under <logHome>/moxy, creating the
// directory. Each named log is opened on its first Printf. A second Enable
// repoints later opens; already-open logs keep their file.
func Enable(logHome string) {
	mu.Lock()
	defer mu.Unlock()
	dir = filepath.Join(logHome, "moxy")
	_ = os.MkdirAll(dir, 0o755)
}

// Dir reports the directory Enable chose, or "" when logging is off. The
// hook's per-session JSONL fire-hose lives beside the named logs and uses it.
func Dir() string {
	mu.Lock()
	defer mu.Unlock()
	return dir
}

// Printf appends one timestamped line to <dir>/<name>.log. A no-op before
// Enable, or when the file cannot be opened: diagnostics never break moxy.
func Printf(name, format string, args ...any) {
	if l := loggerFor(name); l != nil {
		l.Printf(format, args...)
	}
}

// PerProcess returns a log name suffixed with this process's pid, the
// convention the moxin log already used (moxin.<pid>.log) so concurrent moxy
// processes do not interleave.
func PerProcess(name string) string {
	return fmt.Sprintf("%s.%d", name, os.Getpid())
}

func loggerFor(name string) *log.Logger {
	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return nil
	}
	if l, ok := loggers[name]; ok {
		return l
	}
	f, err := os.OpenFile(
		filepath.Join(dir, name+".log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		// Remember the failure so the open is not retried on every line.
		loggers[name] = nil
		return nil
	}
	l := log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	loggers[name] = l
	return l
}
