package mcpclient

import (
	"context"
	"strings"
	"testing"
	"time"
)

// moxy#37: a child that dies before answering initialize leaves its own
// account of why on stderr; the spawn error carries the tail of it, so the
// failed server's status says what happened.
func TestSpawnFailureCarriesTheChildsStderrTail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err := SpawnAndInitialize(ctx, "crasher", "sh",
		[]string{"-c", "echo 'fatal: config missing key foo' >&2; exit 3"})
	if err == nil {
		t.Fatal("SpawnAndInitialize succeeded for a child that exits at once")
	}
	if !strings.Contains(err.Error(), "fatal: config missing key foo") {
		t.Fatalf("error = %q, want the child's stderr in it", err)
	}
}

func TestStderrTailKeepsOnlyTheLastBytes(t *testing.T) {
	tail := newStderrTail(8)
	_, _ = tail.Write([]byte("0123456789"))
	_, _ = tail.Write([]byte("ab"))
	if got := tail.String(); got != "456789ab" {
		t.Fatalf("tail = %q, want the last 8 bytes", got)
	}
}
