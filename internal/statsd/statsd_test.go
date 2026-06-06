package statsd

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func TestSanitizeSegment(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"get-hubbed", "get-hubbed"},
		{"ci-watch", "ci-watch"},
		{"snake_case_OK9", "snake_case_OK9"},
		{"dots.and/slashes here", "dots_and_slashes_here"},
		{"weird|pipe:colon", "weird_pipe_colon"},
		{"", "_"},
	}
	for _, c := range cases {
		if got := sanitizeSegment(c.in); got != c.want {
			t.Errorf("sanitizeSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDispatchPacketFormat(t *testing.T) {
	got := dispatchPacket("get-hubbed", "ci-watch", 1500*time.Millisecond, OutcomeSuccess)
	want := "moxy.get-hubbed.ci-watch.duration:1500|ms\nmoxy.get-hubbed.ci-watch.success:1|c"
	if got != want {
		t.Errorf("dispatchPacket = %q, want %q", got, want)
	}

	got = dispatchPacket("grit", "sub.tool name", 0, OutcomeFailure)
	want = "moxy.grit.sub_tool_name.duration:0|ms\nmoxy.grit.sub_tool_name.failure:1|c"
	if got != want {
		t.Errorf("dispatchPacket = %q, want %q", got, want)
	}

	got = dispatchPacket("builtin", "batch", 42*time.Millisecond, OutcomeAbandoned)
	want = "moxy.builtin.batch.duration:42|ms\nmoxy.builtin.batch.abandoned:1|c"
	if got != want {
		t.Errorf("dispatchPacket = %q, want %q", got, want)
	}
}

// listenUDP binds an ephemeral loopback listener and points the package's
// env config at it.
func listenUDP(t *testing.T) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding UDP listener: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	port := pc.LocalAddr().(*net.UDPAddr).Port
	t.Setenv("STATSD_HOST", "127.0.0.1")
	t.Setenv("STATSD_PORT", strconv.Itoa(port))
	t.Setenv("MOXY_DISABLE_STATSD", "")
	return pc
}

func readPacket(t *testing.T, pc net.PacketConn) string {
	t.Helper()
	buf := make([]byte, 4096)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("reading UDP packet: %v", err)
	}
	return string(buf[:n])
}

func TestEmitToolDispatchOverUDP(t *testing.T) {
	pc := listenUDP(t)
	ReinitFromEnv()

	EmitToolDispatch("get-hubbed", "ci-run-get", 250*time.Millisecond, OutcomeSuccess)

	got := readPacket(t, pc)
	want := "moxy.get-hubbed.ci-run-get.duration:250|ms\nmoxy.get-hubbed.ci-run-get.success:1|c"
	if got != want {
		t.Errorf("received packet %q, want %q", got, want)
	}
}

func TestKillSwitchDisablesEmission(t *testing.T) {
	_ = listenUDP(t)
	t.Setenv("MOXY_DISABLE_STATSD", "1")
	ReinitFromEnv()

	mu.RLock()
	c := conn
	mu.RUnlock()
	if c != nil {
		t.Fatal("expected nil conn with MOXY_DISABLE_STATSD=1")
	}

	// Must be a silent no-op, not a panic.
	EmitToolDispatch("grit", "status", time.Millisecond, OutcomeSuccess)
}

func TestDialFailureRunsDisabled(t *testing.T) {
	// Syntactically invalid address: fails without a DNS lookup, so this
	// behaves identically inside the nix sandbox and on a workstation.
	t.Setenv("STATSD_HOST", "999.999.999.999")
	t.Setenv("STATSD_PORT", "8125")
	t.Setenv("MOXY_DISABLE_STATSD", "")
	ReinitFromEnv()

	mu.RLock()
	c := conn
	mu.RUnlock()
	if c != nil {
		t.Fatal("expected nil conn after dial failure")
	}

	EmitToolDispatch("grit", "status", time.Millisecond, OutcomeFailure)
}
