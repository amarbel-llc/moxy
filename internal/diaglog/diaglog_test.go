package diaglog

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reset returns the package to its never-enabled state between tests.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	dir = ""
	loggers = map[string]*log.Logger{}
}

func TestPrintfIsANoOpUntilEnabled(t *testing.T) {
	reset()
	t.Cleanup(reset)
	Printf("debug", "nothing should be written")
	if got := Dir(); got != "" {
		t.Fatalf("Dir() = %q before Enable, want empty", got)
	}
}

func TestEnableThenPrintfWritesNamedLog(t *testing.T) {
	reset()
	t.Cleanup(reset)
	home := t.TempDir()
	Enable(home)
	Printf("debug", "hello %d", 42)
	Printf("lifecycle", "spawned")

	data, err := os.ReadFile(filepath.Join(home, "moxy", "debug.log"))
	if err != nil {
		t.Fatalf("reading debug.log: %v", err)
	}
	if !strings.Contains(string(data), "hello 42") {
		t.Fatalf("debug.log = %q, want the formatted line", data)
	}
	if _, err := os.Stat(filepath.Join(home, "moxy", "lifecycle.log")); err != nil {
		t.Fatalf("lifecycle.log not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "moxy", "hook.log")); !os.IsNotExist(err) {
		t.Fatalf("hook.log must not exist until something logs to it (err=%v)", err)
	}
}

func TestDefaultLogHomeHonoursXDG(t *testing.T) {
	t.Setenv("XDG_LOG_HOME", "/tmp/xdg-log-home-for-test")
	if got := DefaultLogHome(); got != "/tmp/xdg-log-home-for-test" {
		t.Fatalf("DefaultLogHome() = %q", got)
	}
}
