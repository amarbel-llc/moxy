package proxy

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/statsd"
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

// Resource reads and prompt gets emit their own metric families (#312):
// moxy.<segment>.resource_read.* and moxy.<server>.prompt_get.*.
func TestReadResourceAndGetPromptEmitMetrics(t *testing.T) {
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
	t.Cleanup(statsd.ReinitFromEnv)

	p := &Proxy{}

	// A URI with no server prefix errors before any provider/child lookup
	// — the cheapest read that flows through the wrapper. Segment "_".
	if _, err := p.ReadResource(context.Background(), "justaname"); err == nil {
		t.Fatal("expected error for prefix-less URI")
	}
	buf := make([]byte, 4096)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("reading UDP packet: %v", err)
	}
	got := string(buf[:n])
	if !strings.HasPrefix(got, "moxy._.resource_read.duration:") {
		t.Errorf("packet missing resource_read duration: %q", got)
	}
	if !strings.HasSuffix(got, "moxy._.resource_read.failure:1|c") {
		t.Errorf("packet missing resource_read failure counter: %q", got)
	}

	// Unknown server on a well-formed prompt name → failure under the
	// server's segment.
	if _, err := p.GetPromptV1(context.Background(), "nosuch.prompt", nil); err == nil {
		t.Fatal("expected error for unknown prompt server")
	}
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err = pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("reading UDP packet: %v", err)
	}
	got = string(buf[:n])
	if !strings.HasPrefix(got, "moxy.nosuch.prompt_get.duration:") {
		t.Errorf("packet missing prompt_get duration: %q", got)
	}
	if !strings.HasSuffix(got, "moxy.nosuch.prompt_get.failure:1|c") {
		t.Errorf("packet missing prompt_get failure counter: %q", got)
	}
}

func TestResourceMetricSegment(t *testing.T) {
	cases := []struct{ uri, want string }{
		{"moxy://servers", "moxy"},
		{"madder://blobs/blake2b256-abc", "madder"},
		{"grit/some/resource", "grit"},
		{"justaname", "_"},
		{"", "_"},
	}
	for _, c := range cases {
		if got := resourceMetricSegment(c.uri); got != c.want {
			t.Errorf("resourceMetricSegment(%q) = %q, want %q", c.uri, got, c.want)
		}
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
