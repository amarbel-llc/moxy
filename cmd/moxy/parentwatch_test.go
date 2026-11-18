package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestWatchParentCancelsWhenParentChanges(t *testing.T) {
	var ppid atomic.Int64
	ppid.Store(4242)
	ctx, parentGone := watchParent(context.Background(), 4242, 5*time.Millisecond, func() int { return int(ppid.Load()) })

	if err := parentGone(); err != nil {
		t.Fatalf("parent unchanged: want nil, got %v", err)
	}
	ppid.Store(1) // reparented to init
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context was not cancelled after the parent changed")
	}
	if err := parentGone(); err == nil || !strings.Contains(err.Error(), "4242") {
		t.Fatalf("want an error naming the old parent, got %v", err)
	}
}

func TestWatchParentIgnoresPid1Parent(t *testing.T) {
	// Started as pid 1's child on purpose (a systemd unit): never watch, even
	// when getppid answers something else.
	base := context.Background()
	ctx, parentGone := watchParent(base, 1, time.Millisecond, func() int { return 99 })
	if ctx != base {
		t.Fatal("pid-1 parent: want the context returned unchanged")
	}
	time.Sleep(20 * time.Millisecond)
	if ctx.Err() != nil || parentGone() != nil {
		t.Fatal("pid-1 parent: must keep running")
	}
}

// Real-process check: a parent that dies by SIGKILL takes the watcher's
// context with it. The test binary re-execs itself as parent and child
// (the helper below), so no fixture binary is needed.
func TestWatchParentRealParentKilled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-exit")
	parent := exec.Command(os.Args[0], "-test.run=^TestParentWatchHelper$")
	parent.Env = append(os.Environ(), "MOXY_TEST_PARENTWATCH=parent", "MOXY_TEST_PARENTWATCH_MARKER="+marker)
	out, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	defer parent.Process.Kill()

	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the child's pid: %v", err)
	}
	if _, err := strconv.Atoi(strings.TrimSpace(line)); err != nil {
		t.Fatalf("child pid %q: %v", line, err)
	}

	readyBy := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker + ".ready"); err == nil {
			break
		}
		if time.Now().After(readyBy) {
			t.Fatal("child never recorded its initial parent")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := parent.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(marker); err == nil {
			if !strings.Contains(string(b), "parent process") {
				t.Fatalf("child exited for the wrong reason: %q", b)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("child did not notice its parent was killed within 10s")
}

// TestParentWatchHelper is not a test: it is the entry point the real-process
// test re-execs as "parent" and "child". Without the env var it does nothing.
func TestParentWatchHelper(t *testing.T) {
	switch os.Getenv("MOXY_TEST_PARENTWATCH") {
	case "parent":
		child := exec.Command(os.Args[0], "-test.run=^TestParentWatchHelper$")
		child.Env = append(os.Environ(), "MOXY_TEST_PARENTWATCH=child")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		os.Stdout.WriteString(strconv.Itoa(child.Process.Pid) + "\n")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "child":
		initial := os.Getppid()
		ctx, parentGone := watchParent(context.Background(), initial, 10*time.Millisecond, os.Getppid)
		// Tell the test the initial parent is recorded; it must not kill the
		// parent before this, or the child would start orphaned.
		_ = os.WriteFile(os.Getenv("MOXY_TEST_PARENTWATCH_MARKER")+".ready", []byte(strconv.Itoa(initial)), 0o600)
		select {
		case <-ctx.Done():
			msg := "context cancelled without a parent error"
			if err := parentGone(); err != nil {
				msg = err.Error()
			}
			_ = os.WriteFile(os.Getenv("MOXY_TEST_PARENTWATCH_MARKER"), []byte(msg), 0o600)
		case <-time.After(8 * time.Second):
			// Say what it saw, so a failing run is diagnosable from the
			// parent test's message.
			msg := "no notice: initial ppid " + strconv.Itoa(initial) + ", ppid now " + strconv.Itoa(os.Getppid())
			_ = os.WriteFile(os.Getenv("MOXY_TEST_PARENTWATCH_MARKER"), []byte(msg), 0o600)
		}
		os.Exit(0)
	}
}
