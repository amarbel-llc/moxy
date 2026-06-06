package proxy

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/statsd"
)

// CallToolV1 is the single instrumentation point for dispatch metrics; this
// exercises the wrapper end to end against a real UDP listener.
func TestCallToolV1EmitsDispatchMetrics(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding UDP listener: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	port := pc.LocalAddr().(*net.UDPAddr).Port
	t.Setenv("STATSD_HOST", "127.0.0.1")
	t.Setenv("STATSD_PORT", strconv.Itoa(port))
	t.Setenv("MOXY_DISABLE_STATSD", "")
	statsd.ReinitFromEnv()

	// An unknown server is the cheapest dispatch that flows through the
	// wrapper: it yields a tool-level error result (IsError), so the
	// counter must be .failure.
	p := &Proxy{}
	result, err := p.CallToolV1(context.Background(), "nosuch.tool", nil)
	if err != nil {
		t.Fatalf("CallToolV1: unexpected dispatch error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected IsError result for unknown server, got %+v", result)
	}

	buf := make([]byte, 4096)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("reading UDP packet: %v", err)
	}
	got := string(buf[:n])
	if !strings.HasPrefix(got, "moxy.nosuch.tool.duration:") {
		t.Errorf("packet missing duration line: %q", got)
	}
	if !strings.HasSuffix(got, "moxy.nosuch.tool.failure:1|c") {
		t.Errorf("packet missing failure counter: %q", got)
	}
}

func TestDispatchOutcome(t *testing.T) {
	okResult := &protocol.ToolCallResultV1{}
	errResult := &protocol.ToolCallResultV1{IsError: true}
	boom := errors.New("boom")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name   string
		ctx    context.Context
		result *protocol.ToolCallResultV1
		err    error
		want   statsd.Outcome
	}{
		{"success", context.Background(), okResult, nil, statsd.OutcomeSuccess},
		{"nil result success", context.Background(), nil, nil, statsd.OutcomeSuccess},
		{"dispatch error", context.Background(), nil, boom, statsd.OutcomeFailure},
		{"tool-level error", context.Background(), errResult, nil, statsd.OutcomeFailure},
		{"cancelled ctx with error", cancelled, nil, boom, statsd.OutcomeAbandoned},
		{"cancelled ctx but succeeded", cancelled, okResult, nil, statsd.OutcomeSuccess},
	}
	for _, c := range cases {
		if got := dispatchOutcome(c.ctx, c.result, c.err); got != c.want {
			t.Errorf("%s: dispatchOutcome = %q, want %q", c.name, got, c.want)
		}
	}
}
