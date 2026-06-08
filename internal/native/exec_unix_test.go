//go:build unix

package native

import (
	"context"
	"testing"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// TestRunMoxinProcessKillTreeDoesNotHang is the regression guard for #344/#345.
// The moxin backgrounds a subshell that ignores SIGTERM and keeps the inherited
// stdout pipe open after the direct child (bash) exits. Without process-group
// kill + WaitDelay, cmd.Wait() blocks on that pipe for the full sleep, so a
// cancelled/timed-out async dispatch never returns and the job sits "running".
// With the fix the dispatch unwinds within ~killGrace.
func TestRunMoxinProcessKillTreeDoesNotHang(t *testing.T) {
	orig := killGrace
	killGrace = 200 * time.Millisecond
	t.Cleanup(func() { killGrace = orig })

	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{{
			Name:    "wedge",
			Command: "bash",
			// Background a SIGTERM-ignoring subshell holding stdout, then exit
			// the direct child immediately. Only closing the pipe unblocks Wait.
			Args: []string{"-c", `(trap "" TERM; sleep 10) & exit 0`},
		}},
	}
	srv := NewServer(cfg)

	// A short deadline stands in for an async timeout / cancel.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = srv.Call(ctx, "tools/call", protocol.ToolCallParams{Name: "wedge"})
	}()

	select {
	case <-done:
		// Returned promptly — the process-tree kill / WaitDelay unblocked Wait.
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch hung: process-tree kill / WaitDelay did not unblock cmd.Wait()")
	}
}
