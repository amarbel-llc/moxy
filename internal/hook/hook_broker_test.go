package hook

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.linenisgreat.com/moxy/internal/statsd"
	"code.linenisgreat.com/moxy/pkg/policybroker"
	"code.linenisgreat.com/moxy/pkg/policybroker/fakebroker"
)

// The hook is the policy broker's second client (RFC 0002 section 9): its own
// short-lived process dials, asks per resource and exits, because under
// Claude Code the prompt is decided here, before the call reaches the server.
func dontAskDecision(t *testing.T) string {
	t.Helper()
	out := runHook(t, map[string]any{
		"hook_event_name": "PermissionRequest",
		"session_id":      "broker-" + t.Name(),
		"tool_name":       "mcp__plugin_moxy_moxy__dynres-srv_echo",
		"tool_input":      map[string]any{"path": "/outside/tree"},
		"cwd":             ".",
		"permission_mode": "dontAsk",
	})
	var decoded permissionRequestOutput
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode: %v; raw: %q", err, out)
	}
	return decoded.HookSpecificOutput.Decision.Behavior
}

func hookWithBroker(t *testing.T, addr string) {
	t.Helper()
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask-resource")
	t.Setenv(policybroker.EnvAddr, addr)
	t.Setenv(policybroker.EnvSessionKey, "")
	resetResolverForTest()
	useTempHooksLogDir(t)
}

func TestHook_BrokerVerdictReplacesTheFloor(t *testing.T) {
	// The floor of this fixture is ask, which dontAsk turns into deny.
	for verdict, want := range map[string]string{"allow": "allow", "deny": "deny", "ask": "deny"} {
		t.Run(verdict, func(t *testing.T) {
			f := fakebroker.New(t, "sess")
			f.Answer(func(policybroker.Message) fakebroker.Answer {
				return fakebroker.Answer{Verdict: verdict, Reason: "scripted"}
			})
			hookWithBroker(t, f.Addr())
			if got := dontAskDecision(t); got != want {
				t.Errorf("behavior = %q, want %q", got, want)
			}
			checks := f.Checks()
			if len(checks) != 1 || checks[0].Resource.URI == "" || checks[0].Op != "read" {
				t.Errorf("broker saw %+v, want one read check", checks)
			}
		})
	}
}

func TestHook_NoBrokerAnswerKeepsTheFloor(t *testing.T) {
	// nothing listens: the floor (ask, so deny under dontAsk) decides
	f := fakebroker.New(t, "sess")
	addr := f.Addr()
	f.Close()
	hookWithBroker(t, addr)
	if got := dontAskDecision(t); got != "deny" {
		t.Errorf("behavior = %q, want the floor's deny", got)
	}
}

// listenStatsd points statsd emission at a local UDP socket and returns it.
func listenStatsd(t *testing.T) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no UDP loopback: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	t.Setenv("STATSD_HOST", "127.0.0.1")
	t.Setenv("STATSD_PORT", strconv.Itoa(pc.LocalAddr().(*net.UDPAddr).Port))
	t.Setenv("MOXY_DISABLE_STATSD", "")
	statsd.ReinitFromEnv()
	t.Cleanup(statsd.ReinitFromEnv)
	return pc
}

func drainStatsd(pc net.PacketConn) string {
	var all []string
	buf := make([]byte, 4096)
	for {
		_ = pc.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			return strings.Join(all, "\n")
		}
		all = append(all, string(buf[:n]))
	}
}

func TestHook_ReportsTheDialAndTheCheck(t *testing.T) {
	pc := listenStatsd(t)
	f := fakebroker.New(t, "sess")
	f.Answer(func(policybroker.Message) fakebroker.Answer {
		return fakebroker.Answer{Verdict: "deny"}
	})
	hookWithBroker(t, f.Addr())
	dontAskDecision(t)
	got := drainStatsd(pc)
	for _, want := range []string{
		"moxy.policy_broker.hook.dial.connected:1|c",
		"moxy.policy_broker.hook.check.deny:1|c",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("statsd output lacks %q:\n%s", want, got)
		}
	}
}

func TestHook_ReportsAnUnconnectedDialAndTheFloor(t *testing.T) {
	pc := listenStatsd(t)
	f := fakebroker.New(t, "sess")
	addr := f.Addr()
	f.Close()
	hookWithBroker(t, addr)
	dontAskDecision(t)
	got := drainStatsd(pc)
	for _, want := range []string{
		"moxy.policy_broker.hook.dial.unconnected:1|c",
		"moxy.policy_broker.hook.check.floor:1|c",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("statsd output lacks %q:\n%s", want, got)
		}
	}
}

func TestHook_NoBrokerVariableIsTodaysBehaviour(t *testing.T) {
	hookWithBroker(t, "")
	if got := dontAskDecision(t); got != "deny" {
		t.Errorf("behavior = %q, want deny", got)
	}
}
