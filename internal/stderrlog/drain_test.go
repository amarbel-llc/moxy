package stderrlog

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Init redirects the process's own fd 2, so the tests run it in a helper
// process: this test binary re-executed with MOXY_STDERRLOG_HELPER naming what
// to do. The helper prints its results on stdout.
const helperEnv = "MOXY_STDERRLOG_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "drain":
		helperDrain()
	case "drain-with-holder":
		helperDrainWithHolder()
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// helperDrain writes a line, drains, and exits at once: before Drain existed
// nothing waited for the copier goroutine, so a final line could die in the
// pipe (moxy#438).
func helperDrain() {
	if err := Init("test"); err != nil {
		fmt.Println("init:", err)
		os.Exit(3)
	}
	fmt.Fprintln(os.Stderr, "the final line")
	fmt.Println("drained =", Drain(2*time.Second))
	os.Exit(0)
}

// helperDrainWithHolder starts a child that inherits fd 2 and outlives the
// drain, as a spawned MCP server does: the pipe never reaches EOF, so Drain
// must give up after its timeout instead of hanging.
func helperDrainWithHolder() {
	if err := Init("test"); err != nil {
		fmt.Println("init:", err)
		os.Exit(3)
	}
	// this binary again, in a mode that only sleeps (no `sleep` program is
	// assumed to exist in the test sandbox)
	holder := exec.Command(os.Args[0], "-test.run=^$")
	holder.Env = append(os.Environ(), helperEnv+"=sleep")
	holder.Stderr = os.Stderr
	if err := holder.Start(); err != nil {
		fmt.Println("start:", err)
		os.Exit(3)
	}
	fmt.Fprintln(os.Stderr, "the final line")
	start := time.Now()
	drained := Drain(300 * time.Millisecond)
	fmt.Printf("drained = %v after %v\n", drained, time.Since(start).Round(100*time.Millisecond) <= time.Second)
	_ = holder.Process.Kill() // os.Exit skips defers
	os.Exit(0)
}

func runHelper(t *testing.T, mode string) (stdout, stderr, logHome string) {
	t.Helper()
	logHome = t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		helperEnv+"="+mode,
		"XDG_LOG_HOME="+logHome,
		"SPINCLASS_SESSION_ID=drain-test",
	)
	// Files, not pipes: a child that inherits the helper's stderr and outlives
	// it would keep a pipe open, and cmd.Wait would block on the copy.
	outFile, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = outFile, errFile
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	read := func(f *os.File) string {
		b, _ := os.ReadFile(f.Name())
		return string(b)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("helper %s: %v\nstdout: %s\nstderr: %s", mode, err, read(outFile), read(errFile))
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("helper %s hung\nstdout: %s", mode, read(outFile))
	}
	return read(outFile), read(errFile), logHome
}

func TestDrainDeliversTheFinalLineToTheTerminalAndTheLog(t *testing.T) {
	stdout, stderr, logHome := runHelper(t, "drain")
	if !strings.Contains(stdout, "drained = true") {
		t.Fatalf("Drain did not report the copier finished: %q", stdout)
	}
	if !strings.Contains(stderr, "the final line") {
		t.Errorf("the final line never reached the terminal: %q", stderr)
	}
	log, err := os.ReadFile(filepath.Join(logHome, "moxy", "stderr", "active", "drain-test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "the final line") {
		t.Errorf("the final line never reached the session log: %q", log)
	}
}

func TestDrainGivesUpWhenAChildHoldsStderrOpen(t *testing.T) {
	stdout, stderr, _ := runHelper(t, "drain-with-holder")
	if !strings.Contains(stdout, "drained = false after true") {
		t.Errorf("Drain should time out (false) well inside a second: %q", stdout)
	}
	if !strings.Contains(stderr, "the final line") {
		t.Errorf("the line written before the drain was lost: %q", stderr)
	}
}
