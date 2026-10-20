package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type permissionRequestOutput struct {
	HookSpecificOutput struct {
		HookEventName string `json:"hookEventName"`
		Decision      struct {
			Behavior string `json:"behavior"`
			Message  string `json:"message"`
		} `json:"decision"`
	} `json:"hookSpecificOutput"`
}

// useTempHooksLogDir points the per-session hook log at a temp dir for the
// test and returns that dir.
func useTempHooksLogDir(t *testing.T) string {
	t.Helper()
	prev := hooksLogDir
	hooksLogDir = t.TempDir()
	t.Cleanup(func() { hooksLogDir = prev })
	return hooksLogDir
}

func runHook(t *testing.T, input map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	// No fall-through to go-mcp's HandleHook is expected in these cases, so
	// no command.App is needed.
	if err := Handle(nil, bytes.NewReader(raw), &out); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return out.Bytes()
}

func readHookLogDecisions(t *testing.T, dir, sessionID string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("read hook log: %v", err)
	}
	var decisions []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("hook log line %q: %v", line, err)
		}
		if rec["hook_event_name"] == "MoxyDecision" {
			decisions = append(decisions, rec)
		}
	}
	return decisions
}

// #484: a dynamic tool whose check resolves to ask must be denied at once
// when the PermissionRequest arrives under dontAsk, never left pending.
func TestHandle_PermissionRequestDontAsk_DynamicAskIsDenied(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask")
	resetResolverForTest()
	logDir := useTempHooksLogDir(t)

	out := runHook(t, map[string]any{
		"hook_event_name": "PermissionRequest",
		"session_id":      "dontask-ask",
		"tool_name":       "mcp__plugin_moxy_moxy__dynask-srv_echo",
		"tool_input":      map[string]any{"path": "/outside/tree"},
		"cwd":             ".",
		"permission_mode": "dontAsk",
	})

	var decoded permissionRequestOutput
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode: %v; raw: %q", err, out)
	}
	hso := decoded.HookSpecificOutput
	if hso.HookEventName != "PermissionRequest" {
		t.Errorf("hookEventName = %q, want PermissionRequest", hso.HookEventName)
	}
	if hso.Decision.Behavior != "deny" {
		t.Fatalf("behavior = %q, want deny", hso.Decision.Behavior)
	}
	for _, want := range []string{"dynask-srv_echo", "/outside/tree", "dontAsk"} {
		if !strings.Contains(hso.Decision.Message, want) {
			t.Errorf("message %q does not mention %q", hso.Decision.Message, want)
		}
	}

	decisions := readHookLogDecisions(t, logDir, "dontask-ask")
	if len(decisions) != 1 || decisions[0]["decision"] != "deny" || decisions[0]["for_event"] != "PermissionRequest" {
		t.Errorf("hook log decisions = %v, want one PermissionRequest deny", decisions)
	}
}

// The dynamic check still runs under dontAsk: an allow outcome is honoured.
func TestHandle_PermissionRequestDontAsk_DynamicAllowIsAllowed(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic")
	resetResolverForTest()
	logDir := useTempHooksLogDir(t)

	out := runHook(t, map[string]any{
		"hook_event_name": "PermissionRequest",
		"session_id":      "dontask-allow",
		"tool_name":       "mcp__plugin_moxy_moxy__dyn-srv_echo",
		"tool_input":      map[string]any{"path": "inside"},
		"cwd":             ".",
		"permission_mode": "dontAsk",
	})

	var decoded permissionRequestOutput
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode: %v; raw: %q", err, out)
	}
	if got := decoded.HookSpecificOutput.Decision.Behavior; got != "allow" {
		t.Fatalf("behavior = %q, want allow", got)
	}

	decisions := readHookLogDecisions(t, logDir, "dontask-allow")
	if len(decisions) != 1 || decisions[0]["decision"] != "allow" {
		t.Errorf("hook log decisions = %v, want one allow", decisions)
	}
}

// Outside dontAsk the PermissionRequest response stays empty, so Claude Code
// shows its normal dialog.
func TestHandle_PermissionRequestDefaultMode_StaysEmpty(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask")
	resetResolverForTest()
	useTempHooksLogDir(t)

	out := runHook(t, map[string]any{
		"hook_event_name": "PermissionRequest",
		"session_id":      "default-mode",
		"tool_name":       "mcp__plugin_moxy_moxy__dynask-srv_echo",
		"tool_input":      map[string]any{},
		"permission_mode": "default",
	})
	if len(out) != 0 {
		t.Fatalf("expected empty output outside dontAsk, got %q", out)
	}
}

// PreToolUse under dontAsk turns the ask into a deny before Claude Code
// would raise a PermissionRequest at all.
func TestHandle_PreToolUseDontAsk_AskIsDowngradedToDeny(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask")
	resetResolverForTest()
	logDir := useTempHooksLogDir(t)

	out := runHook(t, map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "pretool-dontask",
		"tool_name":       "mcp__plugin_moxy_moxy__dynask-srv_echo",
		"tool_input":      map[string]any{"path": "/outside/tree"},
		"cwd":             ".",
		"permission_mode": "dontAsk",
	})

	var decoded hookOutput
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode: %v; raw: %q", err, out)
	}
	if got := decoded.HookSpecificOutput.PermissionDecision; got != "deny" {
		t.Fatalf("decision = %q, want deny", got)
	}
	if !strings.Contains(decoded.HookSpecificOutput.PermissionDecisionReason, "dontAsk") {
		t.Errorf("reason %q does not mention dontAsk", decoded.HookSpecificOutput.PermissionDecisionReason)
	}

	decisions := readHookLogDecisions(t, logDir, "pretool-dontask")
	if len(decisions) != 1 || decisions[0]["decision"] != "deny" {
		t.Errorf("hook log decisions = %v, want one deny", decisions)
	}
}

// PreToolUse outside dontAsk keeps the ask.
func TestHandle_PreToolUseDefaultMode_KeepsAsk(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask")
	resetResolverForTest()
	useTempHooksLogDir(t)

	out := runHook(t, map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "pretool-default",
		"tool_name":       "mcp__plugin_moxy_moxy__dynask-srv_echo",
		"tool_input":      map[string]any{},
		"cwd":             ".",
		"permission_mode": "default",
	})

	var decoded hookOutput
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("decode: %v; raw: %q", err, out)
	}
	if got := decoded.HookSpecificOutput.PermissionDecision; got != "ask" {
		t.Fatalf("decision = %q, want ask", got)
	}
}
