package hook

import (
	"encoding/json"
	"testing"

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

func TestHook_NoBrokerVariableIsTodaysBehaviour(t *testing.T) {
	hookWithBroker(t, "")
	if got := dontAskDecision(t); got != "deny" {
		t.Errorf("behavior = %q, want deny", got)
	}
}
