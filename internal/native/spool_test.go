package native

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/moxy/internal/spoolctx"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

// A native dispatch whose context carries a spool path tees the child's
// stdout AND stderr into the spool, interleaved (RFC-0010 / FDR-0005). The
// in-memory result path is unaffected (stdout stays its own buffer, #338).
func TestRunMoxinProcessTeesToSpool(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "job.out")
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{{
			Name:       "emit",
			Command:    "bash",
			Args:       []string{"-c", `echo out-line; echo err-line >&2`},
			ResultType: ResultTypeText,
		}},
	}
	srv := NewServer(cfg)

	ctx := spoolctx.WithPath(context.Background(), spool)
	if _, err := srv.Call(ctx, "tools/call", protocol.ToolCallParams{Name: "emit"}); err != nil {
		t.Fatalf("Call: %v", err)
	}

	data, err := os.ReadFile(spool)
	if err != nil {
		t.Fatalf("reading spool: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "out-line") {
		t.Errorf("spool missing stdout: %q", got)
	}
	if !strings.Contains(got, "err-line") {
		t.Errorf("spool missing stderr: %q", got)
	}
}

// The spool is append-only: a second dispatch to the same path adds to it
// rather than truncating (RFC-0010 §1; batch-async runs many sub-calls).
func TestRunMoxinProcessSpoolAppends(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "job.out")
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{{
			Name:       "emit",
			Command:    "bash",
			Args:       []string{"-c", `echo line`},
			ResultType: ResultTypeText,
		}},
	}
	srv := NewServer(cfg)
	ctx := spoolctx.WithPath(context.Background(), spool)
	for range 2 {
		if _, err := srv.Call(ctx, "tools/call", protocol.ToolCallParams{Name: "emit"}); err != nil {
			t.Fatalf("Call: %v", err)
		}
	}
	data, _ := os.ReadFile(spool)
	if n := strings.Count(string(data), "line"); n != 2 {
		t.Errorf("spool has %d lines, want 2 (append, not truncate): %q", n, data)
	}
}

// No spool path on the context → no spool written, no error (the common
// non-async path, and async when clown is disabled/absent).
func TestRunMoxinProcessNoSpoolWhenAbsent(t *testing.T) {
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{{
			Name:       "emit",
			Command:    "bash",
			Args:       []string{"-c", `echo hi`},
			ResultType: ResultTypeText,
		}},
	}
	srv := NewServer(cfg)
	if _, err := srv.Call(context.Background(), "tools/call", protocol.ToolCallParams{Name: "emit"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
}
