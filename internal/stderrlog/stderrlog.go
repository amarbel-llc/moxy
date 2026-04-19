// Package stderrlog redirects moxy's os.Stderr (and Go runtime panic traces)
// to a per-session log file so that crashes that bypass normal logging can be
// recovered after the fact.
//
// Files are written under $XDG_LOG_HOME/moxy/stderr/ in two states:
//
//   - active/<session-id>.log     — opened on Init, written to for the life of the process
//   - completed/<session-id>.log  — after a clean Rotate (shutdown path or SessionEnd hook)
//
// A companion <session-id>.log.pid sidecar stores the owning pid. On startup
// Init sweeps active/ and moves any file whose pid is no longer alive to
// completed/<session-id>.orphan.log, so the set of files remaining in active/
// always reflects currently-live moxy processes.
//
// Session id comes from $SPINCLASS_SESSION_ID, falling back to "pid-<pid>".
// Slashes in the session id are preserved as directory separators so that
// e.g. SPINCLASS_SESSION_ID="moxy/proud-chestnut" becomes
// active/moxy/proud-chestnut.log.
package stderrlog

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	mu          sync.Mutex
	initialized bool
	sessionID   string
	activePath  string
	completedPath string
	pidSidecar  string
)

// Init opens the per-session stderr log and dup2's fd 2 onto it so os.Stderr,
// the log package, and Go runtime panic traces all land in the file. Creates
// the active/ and completed/ directories if missing and sweeps any orphaned
// files (pid no longer alive) on the way in. Idempotent.
func Init(version string) error {
	mu.Lock()
	defer mu.Unlock()
	if initialized {
		return nil
	}

	sessionID = resolveSessionID()
	root := baseDir()
	activeDir := filepath.Join(root, "active")
	completedDir := filepath.Join(root, "completed")

	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		return fmt.Errorf("mkdir active: %w", err)
	}
	if err := os.MkdirAll(completedDir, 0o755); err != nil {
		return fmt.Errorf("mkdir completed: %w", err)
	}

	sweepOrphans(activeDir, completedDir)

	key := sanitize(sessionID)
	activePath = filepath.Join(activeDir, key+".log")
	completedPath = filepath.Join(completedDir, key+".log")
	pidSidecar = activePath + ".pid"

	if err := os.MkdirAll(filepath.Dir(activePath), 0o755); err != nil {
		return fmt.Errorf("mkdir active session parent: %w", err)
	}

	f, err := os.OpenFile(activePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open stderr log: %w", err)
	}

	// Tee fd 2 so stderr writes land in the log file AND continue to the
	// original stderr: preserves terminal visibility (bats tests, interactive
	// runs, Claude Code's captured stderr) while still capturing everything —
	// including Go runtime panic traces, which write directly to fd 2 — to
	// the file.
	//
	// Implementation: save the original fd 2 to a separate fd, create a pipe,
	// point fd 2 at the pipe's write end, and fan out the pipe's read end to
	// the log file and the saved original stderr in a goroutine.
	origStderrFd, err := syscall.Dup(2)
	if err != nil {
		f.Close()
		return fmt.Errorf("dup original stderr: %w", err)
	}
	origStderr := os.NewFile(uintptr(origStderrFd), "stderr-orig")

	pr, pw, err := os.Pipe()
	if err != nil {
		origStderr.Close()
		f.Close()
		return fmt.Errorf("pipe: %w", err)
	}
	if err := syscall.Dup2(int(pw.Fd()), 2); err != nil {
		pr.Close()
		pw.Close()
		origStderr.Close()
		f.Close()
		return fmt.Errorf("dup2 pipe onto stderr: %w", err)
	}
	pw.Close() // fd 2 now holds the write end; the Go *File is unneeded.

	go func() {
		defer pr.Close()
		defer f.Close()
		defer origStderr.Close()
		_, _ = io.Copy(io.MultiWriter(f, origStderr), pr)
	}()

	if err := os.WriteFile(pidSidecar, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("write pid sidecar: %w", err)
	}

	fmt.Fprintf(
		os.Stderr,
		"--- moxy started pid=%d session=%q version=%s at=%s ---\n",
		os.Getpid(), sessionID, version, time.Now().Format(time.RFC3339Nano),
	)

	initialized = true
	return nil
}

// Rotate moves the active stderr log to completed/ and removes the pid
// sidecar. Called from moxy's shutdown path. Idempotent; safe to call even if
// Init was never called.
func Rotate() {
	mu.Lock()
	defer mu.Unlock()
	if !initialized {
		return
	}
	fmt.Fprintf(os.Stderr, "--- moxy stopping pid=%d session=%q at=%s ---\n",
		os.Getpid(), sessionID, time.Now().Format(time.RFC3339Nano))
	rotateFile(activePath, completedPath)
	os.Remove(pidSidecar)
	initialized = false
}

// RotateBySessionID is used from the SessionEnd hook handler, which runs in a
// separate moxy process with no in-memory state from the main moxy process.
// Looks up the file paths purely from the provided session id.
func RotateBySessionID(sessionID string) {
	if sessionID == "" {
		return
	}
	root := baseDir()
	key := sanitize(sessionID)
	active := filepath.Join(root, "active", key+".log")
	completed := filepath.Join(root, "completed", key+".log")
	rotateFile(active, completed)
	os.Remove(active + ".pid")
}

func resolveSessionID() string {
	if id := strings.TrimSpace(os.Getenv("SPINCLASS_SESSION_ID")); id != "" {
		return id
	}
	return fmt.Sprintf("pid-%d", os.Getpid())
}

func baseDir() string {
	logHome := os.Getenv("XDG_LOG_HOME")
	if logHome == "" {
		home, _ := os.UserHomeDir()
		logHome = filepath.Join(home, ".local", "log")
	}
	return filepath.Join(logHome, "moxy", "stderr")
}

// sanitize keeps '/' as a directory separator so that a session id like
// "moxy/proud-chestnut" becomes a subdirectory hierarchy. Any character
// outside [A-Za-z0-9/._-] is replaced with '_'. Leading/trailing slashes and
// ".." segments are stripped so we can't escape the stderr root.
func sanitize(s string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r == '/', r == '-', r == '_', r == '.':
			return r
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, s)
	mapped = strings.ReplaceAll(mapped, "..", "_")
	mapped = strings.Trim(mapped, "/")
	if mapped == "" {
		return fmt.Sprintf("pid-%d", os.Getpid())
	}
	return mapped
}

func rotateFile(activePath, completedPath string) {
	if _, err := os.Stat(activePath); err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(completedPath), 0o755); err != nil {
		return
	}
	_ = os.Rename(activePath, completedPath)
}

// sweepOrphans walks active/ looking for .log files whose .pid sidecar points
// to a process that no longer exists, and moves them to completed/ with a
// .orphan.log suffix. Failures are silent — orphan sweep is best-effort.
func sweepOrphans(activeDir, completedDir string) {
	_ = filepath.WalkDir(activeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".log") {
			return nil
		}
		pid := readPidSidecar(path + ".pid")
		if pid > 0 && pidIsAlive(pid) {
			return nil
		}
		rel, err := filepath.Rel(activeDir, path)
		if err != nil {
			return nil
		}
		dst := filepath.Join(completedDir, strings.TrimSuffix(rel, ".log")+".orphan.log")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil
		}
		_ = os.Rename(path, dst)
		_ = os.Remove(path + ".pid")
		return nil
	})
}

func readPidSidecar(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

// pidIsAlive checks whether a process with the given pid currently exists on
// this system. Linux-specific via /proc.
func pidIsAlive(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}
