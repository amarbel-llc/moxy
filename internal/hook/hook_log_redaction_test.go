package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moxy#504: the per-session hook log keeps what a call was (tool, event,
// timing, the decision) and never its inputs: every session can read every
// other session's log, so a tool_input there would hand one session's file
// contents and message bodies to the next. MOXY_HOOK_LOG_INPUTS=1 is the
// opt-in that keeps the raw event for debugging.
func TestHookLogKeepsWhatHappenedNotTheInputs(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask")
	t.Setenv("MOXY_HOOK_LOG_INPUTS", "")
	resetResolverForTest()
	logDir := useTempHooksLogDir(t)

	const secret = "the-body-of-a-private-message"
	runHook(t, map[string]any{
		"hook_event_name":        "PermissionRequest",
		"session_id":             "redacted",
		"tool_name":              "mcp__plugin_moxy_moxy__dynask-srv_echo",
		"tool_input":             map[string]any{"path": "/outside/tree", "content": secret},
		"tool_use_id":            "tu-1",
		"permission_suggestions": []any{map[string]any{"rule": secret}},
		"cwd":                    ".",
		"permission_mode":        "dontAsk",
	})

	path := filepath.Join(logDir, "redacted.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hook log: %v", err)
	}
	log := string(data)
	for _, leaked := range []string{secret, "/outside/tree", "tool_input", "permission_suggestions", "reason"} {
		if strings.Contains(log, leaked) {
			t.Errorf("hook log holds %q:\n%s", leaked, log)
		}
	}
	for _, kept := range []string{
		`"hook_event_name":"PermissionRequest"`, `"tool_name":"mcp__plugin_moxy_moxy__dynask-srv_echo"`,
		`"tool_use_id":"tu-1"`, `"ts":`, `"hook_event_name":"MoxyDecision"`, `"decision":"deny"`,
	} {
		if !strings.Contains(log, kept) {
			t.Errorf("hook log lacks %s:\n%s", kept, log)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("hook log mode = %v, want readable by its owner only", mode)
	}
}

func TestHookLogKeepsTheInputsOnlyWhenAskedTo(t *testing.T) {
	t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-ask")
	t.Setenv("MOXY_HOOK_LOG_INPUTS", "1")
	resetResolverForTest()
	logDir := useTempHooksLogDir(t)

	runHook(t, map[string]any{
		"hook_event_name": "PermissionRequest",
		"session_id":      "full",
		"tool_name":       "mcp__plugin_moxy_moxy__dynask-srv_echo",
		"tool_input":      map[string]any{"path": "/outside/tree"},
		"cwd":             ".",
		"permission_mode": "dontAsk",
	})
	data, err := os.ReadFile(filepath.Join(logDir, "full.jsonl"))
	if err != nil {
		t.Fatalf("read hook log: %v", err)
	}
	for _, want := range []string{`"tool_input":{"path":"/outside/tree"}`, `"reason":`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("hook log with MOXY_HOOK_LOG_INPUTS=1 lacks %s:\n%s", want, data)
		}
	}
}
