package main

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amarbel-llc/moxy/internal/native"
	"github.com/amarbel-llc/moxy/internal/statsd"
)

// Regression for #311: standalone-moxin serving (serve_moxin) dispatches
// through native.ToolAdapter directly, bypassing the proxy's CallToolV1
// instrumentation point — the instrumented wrapper must emit the same
// moxy.<server>.<tool>.* metrics.
func TestInstrumentedToolAdapterEmitsDispatchMetrics(t *testing.T) {
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

	srv := native.NewServer(&native.NativeConfig{
		Name: "fixturemoxin",
		Tools: []native.ToolSpec{{
			Name:       "echo",
			Command:    "echo",
			Args:       []string{"-n", "hi"},
			ResultType: native.ResultTypeText,
		}},
	})
	adapter := &instrumentedToolAdapter{
		ToolAdapter: &native.ToolAdapter{Srv: srv},
		server:      "fixturemoxin",
	}

	result, err := adapter.CallToolV1(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("CallToolV1: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected success result, got %+v", result)
	}

	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1024)
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("reading statsd packet: %v", err)
	}
	packet := string(buf[:n])

	if !strings.Contains(packet, "moxy.fixturemoxin.echo.duration:") {
		t.Errorf("packet missing duration metric: %q", packet)
	}
	if !strings.Contains(packet, "moxy.fixturemoxin.echo.success:1|c") {
		t.Errorf("packet missing success counter: %q", packet)
	}
}
