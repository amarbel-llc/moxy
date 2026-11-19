//go:build linux

package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A stdio child must not outlive moxy, even when moxy dies without unwinding
// (SIGKILL, OOM; moxy#527). The fixture child answers the handshake and then
// ignores everything, stdin EOF included, so only the kernel's parent-death
// signal can end it. The test binary re-execs itself as both roles.

func TestChildDiesWhenMoxyIsKilled(t *testing.T) {
	moxy := exec.Command(os.Args[0], "-test.run=^TestPdeathsigHelper$")
	moxy.Env = append(os.Environ(), "MOXY_TEST_PDEATHSIG=moxy")
	out, err := moxy.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := moxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer moxy.Process.Kill()

	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the child's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(line), "child "))
	if err != nil {
		t.Fatalf("child pid from %q: %v", line, err)
	}
	// Whatever happens next, do not leave the fixture running.
	defer syscall.Kill(pid, syscall.SIGKILL)

	if !processAlive(pid) {
		t.Fatalf("fixture child %d is not running before moxy is killed", pid)
	}

	if err := moxy.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = moxy.Wait()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child %d still running 10 s after moxy was SIGKILLed", pid)
}

// processAlive reports whether pid is a live process; a zombie waiting for its
// reaper (the sandbox's init may not reap) counts as dead.
func processAlive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return false
	}
	return s[i+2] != 'Z'
}

// TestPdeathsigHelper is not a test: it is the entry point the test re-execs as
// "moxy" (spawning a child through SpawnAndInitialize) and as "child" (an MCP
// server that answers initialize and then ignores stdin). Without the env var
// it does nothing.
func TestPdeathsigHelper(t *testing.T) {
	switch os.Getenv("MOXY_TEST_PDEATHSIG") {
	case "moxy":
		os.Setenv("MOXY_TEST_PDEATHSIG", "child")
		c, _, err := SpawnAndInitialize(context.Background(), "fixture",
			os.Args[0], []string{"-test.run=^TestPdeathsigHelper$"})
		if err != nil {
			os.Stderr.WriteString("spawn: " + err.Error() + "\n")
			os.Exit(2)
		}
		os.Stdout.WriteString("child " + strconv.Itoa(c.pid) + "\n")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "child":
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(sc.Bytes(), &req) != nil || req.Method != "initialize" {
				continue
			}
			resp := `{"jsonrpc":"2.0","id":` + string(req.ID) +
				`,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"fixture","version":"0"}}}`
			os.Stdout.WriteString(resp + "\n")
			break
		}
		// Stop reading: stdin EOF no longer matters. Only a parent-death
		// signal ends this process now.
		time.Sleep(time.Hour)
		os.Exit(0)
	}
}
