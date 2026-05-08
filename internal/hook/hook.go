package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"

	"github.com/amarbel-llc/moxy/internal/native"
	"github.com/amarbel-llc/moxy/internal/stderrlog"
)

var (
	hookLog     *log.Logger
	hooksLogDir string
)

func init() {
	logHome := os.Getenv("XDG_LOG_HOME")
	if logHome == "" {
		home, _ := os.UserHomeDir()
		logHome = filepath.Join(home, ".local", "log")
	}
	logDir := filepath.Join(logHome, "moxy")
	os.MkdirAll(logDir, 0o755)
	f, err := os.OpenFile(
		filepath.Join(logDir, "hook.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err == nil {
		hookLog = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	}
	hooksLogDir = filepath.Join(logDir, "hooks")
}

// logHookEvent appends the raw hook input (with a timestamp added) to
// a per-session JSONL file at ~/.local/log/moxy/hooks/{session_id}.jsonl.
// This is a fire-hose log of every hook invocation — downstream tools
// like freud filter by hook_event_name as needed.
func logHookEvent(raw json.RawMessage, hi hookInput) {
	if hi.SessionID == "" {
		return
	}

	// Re-marshal with timestamp injected.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		debugHook("logHookEvent: unmarshal error: %v", err)
		return
	}
	obj["ts"] = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(obj)
	if err != nil {
		debugHook("logHookEvent: marshal error: %v", err)
		return
	}

	if err := os.MkdirAll(hooksLogDir, 0o755); err != nil {
		debugHook("logHookEvent: mkdir error: %v", err)
		return
	}

	logPath := filepath.Join(hooksLogDir, hi.SessionID+".jsonl")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		debugHook("logHookEvent: open error: %v", err)
		return
	}
	defer f.Close()

	f.Write(data)
	f.Write([]byte("\n"))
}

func debugHook(format string, args ...any) {
	if hookLog != nil {
		hookLog.Printf(format, args...)
	}
}

// hookInput mirrors the unexported type in go-mcp/command.
type hookInput struct {
	HookEventName string         `json:"hook_event_name"`
	SessionID     string         `json:"session_id"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
	AgentID       string         `json:"agent_id,omitempty"`
	AgentType     string         `json:"agent_type,omitempty"`
}

type hookOutput struct {
	HookSpecificOutput hookDecision `json:"hookSpecificOutput"`
}

type hookDecision struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// moxyToolPrefixes lists all known Claude Code naming patterns for moxy
// tools. The direct MCP config uses "mcp__moxy__", while plugin-loaded
// moxy uses "mcp__plugin_moxy_moxy__" (or "mcp__plugin_<name>_moxy__").
var moxyToolPrefixes = []string{
	"mcp__moxy__",
	"mcp__plugin_moxy_moxy__",
}

// matchMoxyPrefix returns the prefix that toolName starts with, or ""
// if it doesn't match any known moxy tool pattern.
func matchMoxyPrefix(toolName string) string {
	for _, p := range moxyToolPrefixes {
		if strings.HasPrefix(toolName, p) {
			return p
		}
	}
	return ""
}

// Handle processes hook invocations from Claude Code.
//
// Every invocation is logged to a per-session JSONL file in
// ~/.local/log/moxy/hooks/ (see moxy-hooks(5) for the format).
//
// Then dispatches based on hook_event_name:
//
//   - PreToolUse: permission decisions for moxin tools, then falls through to
//     go-mcp's HandleHook for deny-redirect logic.
//   - PermissionRequest: returns empty output so Claude Code shows the normal
//     permission dialog (the event is already logged above).
//   - All other events: logged only (no response needed).
//
// The hook matcher in hooks.json is ".*" so this fires for all tools.
// Non-moxy tools fall through immediately.
//
// Follows fail-open: any error silently falls through to app.HandleHook.
func Handle(app *command.App, r io.Reader, w io.Writer) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		log.Printf("hook: ignoring read error (fail-open): %v", err)
		return nil
	}

	var hi hookInput
	if err := json.Unmarshal(raw, &hi); err != nil {
		// Can't parse — fall through to go-mcp handler which has its own
		// error handling.
		return app.HandleHook(bytes.NewReader(raw), w)
	}

	debugHook("event=%q tool_name=%q", hi.HookEventName, hi.ToolName)

	// SessionEnd has no tool name, so it must be handled before the moxy
	// tool-prefix filter. Rotate the active stderr log for this session to
	// completed/ so that any file left behind in active/ indicates a moxy
	// that was killed before its session ended cleanly.
	if hi.HookEventName == "SessionEnd" {
		stderrlog.RotateBySessionID(os.Getenv("SPINCLASS_SESSION_ID"))
		return nil
	}

	// Only process moxy tools — non-moxy tools fall through immediately.
	prefix := matchMoxyPrefix(hi.ToolName)
	if prefix == "" {
		return nil
	}

	// Fire-hose: log every moxy hook event to per-session JSONL.
	logHookEvent(json.RawMessage(raw), hi)

	switch hi.HookEventName {
	case "PermissionRequest":
		// Return empty output — Claude Code shows the normal permission dialog.
		return nil

	default:
		// PreToolUse (and any future event types): existing behavior.
		parsed, ok := parseNativeToolName(hi.ToolName, prefix)
		debugHook("  parsed=%q ok=%v", parsed, ok)
		if tryBuiltinAutoAllow(hi.ToolName, prefix, w) {
			debugHook("  decision: builtin auto-allowed")
			return nil
		}
		if tryPermsDecision(hi.ToolName, prefix, hi.ToolInput, w) {
			debugHook("  decision: allowed")
			return nil
		}
		debugHook("  decision: fall-through")

		return app.HandleHook(bytes.NewReader(raw), w)
	}
}

// builtinAutoAllow lists builtin (non-moxin) tool names that are
// always allowed without a permission prompt. These are the suffix
// after stripping the moxy prefix (e.g. "status" from
// "mcp__plugin_moxy_moxy__status").
var builtinAutoAllow = map[string]bool{
	"status": true,
}

// tryBuiltinAutoAllow checks whether the tool is a builtin that should be
// auto-allowed. Returns true if it wrote a decision, false to fall through.
func tryBuiltinAutoAllow(toolName, prefix string, w io.Writer) bool {
	suffix := strings.TrimPrefix(toolName, prefix)
	if suffix == toolName || !builtinAutoAllow[suffix] {
		return false
	}

	out := hookOutput{
		HookSpecificOutput: hookDecision{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "allow",
			PermissionDecisionReason: "auto-allowed builtin tool",
		},
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("hook: ignoring encode error (fail-open): %v", err)
		return false
	}
	return true
}

// tryPermsDecision checks the tool's perms-request in moxin configs and writes
// the corresponding hook decision. Returns true if it wrote a decision, false
// to fall through to the client.
func tryPermsDecision(toolName, prefix string, toolInput map[string]any, w io.Writer) bool {
	serverTool, ok := parseNativeToolName(toolName, prefix)
	if !ok {
		return false
	}

	perms := discoverPermissions()
	debugHook("  perms map has %d entries, looking up %q", len(perms), serverTool)
	if len(perms) > 0 {
		keys := make([]string, 0, len(perms))
		for k := range perms {
			keys = append(keys, k)
		}
		debugHook("  perms keys: %v", keys)
	}
	info, exists := perms[serverTool]
	debugHook("  lookup %q: exists=%v perm=%q", serverTool, exists, info.Perm)
	if !exists {
		return false // delegate-to-client: fall through
	}

	var decision, reason string
	switch info.Perm {
	case native.PermsAlwaysAllow:
		decision = "allow"
		reason = "always-allow by moxin config"
	case native.PermsEachUse:
		decision = "ask"
		reason = "each-use: requires explicit approval"
	case native.PermsDynamic:
		decision, reason = evalDynamicForHook(info.DynamicPerms, toolInput)
		debugHook("  dynamic eval: decision=%q reason=%q", decision, reason)
		if decision == "" {
			return false // fall-through (script returned an unmapped exit)
		}
	default:
		return false // delegate-to-client or unrecognized: fall through
	}

	out := hookOutput{
		HookSpecificOutput: hookDecision{
			HookEventName:            "PreToolUse",
			PermissionDecision:       decision,
			PermissionDecisionReason: reason,
		},
	}

	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("hook: ignoring encode error (fail-open): %v", err)
		return false
	}

	return true
}

// evalDynamicForHook runs the per-tool dynamic-perms predicate and maps its
// decision into the (decision, reason) shape Claude Code expects on the
// PreToolUse hook. Returns ("", reason) for fall-through (unmapped exit).
func evalDynamicForHook(spec *native.DynamicPermsSpec, toolInput map[string]any) (string, string) {
	if spec == nil {
		// Validator should have caught this at config-load time, but be
		// defensive: if a tool somehow declared `dynamic` without a
		// `[dynamic-perms]` block, fall through to the client.
		return "", "dynamic-perms: no [dynamic-perms] spec on tool"
	}

	args, err := json.Marshal(toolInput)
	if err != nil {
		return "ask", fmt.Sprintf("dynamic-perms: failed to re-marshal tool input: %v", err)
	}

	dec, reason := native.EvalDynamicPerms(context.Background(), spec, nil, args)
	switch dec {
	case native.DynPermsAllow:
		return "allow", reason
	case native.DynPermsAsk:
		return "ask", reason
	case native.DynPermsDeny:
		return "deny", reason
	default:
		return "", reason
	}
}

// parseNativeToolName strips the given prefix and converts the remainder
// to "server.tool" form. For example:
//
//	"mcp__moxy__folio_read"                → "folio.read"
//	"mcp__plugin_moxy_moxy__folio_read"    → "folio.read"
//
// Server names may contain hyphens but not underscores or dots, so the first
// underscore after the prefix separates server name from tool name.
func parseNativeToolName(toolName, prefix string) (string, bool) {
	suffix := strings.TrimPrefix(toolName, prefix)
	if suffix == toolName {
		return "", false
	}

	idx := strings.IndexByte(suffix, '_')
	if idx < 0 {
		// No underscore means no tool name part — could be a meta tool
		// like "restart" which has no server prefix in native form.
		return "", false
	}

	server := suffix[:idx]
	tool := suffix[idx+1:]
	if server == "" || tool == "" {
		return "", false
	}

	return server + "." + tool, true
}

// toolPermInfo carries everything tryPermsDecision needs to make a hook-time
// decision for one tool. For non-dynamic perms the Perm field is enough; for
// `dynamic` the DynamicPerms spec is required so we can spawn the predicate.
type toolPermInfo struct {
	Perm         native.PermsRequest
	DynamicPerms *native.DynamicPermsSpec
}

// discoverPermissions loads moxin configs and returns a map of
// "server.tool" names to their perm info. Only tools with an explicit
// perms-request are included.
func discoverPermissions() map[string]toolPermInfo {
	moxinPath := os.Getenv("MOXIN_PATH")
	systemDir := native.SystemMoxinDir()
	debugHook("  discoverPermissions: MOXIN_PATH=%q systemDir=%q", moxinPath, systemDir)
	configs, err := native.DiscoverConfigs(moxinPath, systemDir)
	if err != nil {
		debugHook("  discoverPermissions error: %v", err)
		return nil
	}
	debugHook("  discoverPermissions: found %d configs", len(configs))

	perms := make(map[string]toolPermInfo)
	for _, cfg := range configs {
		for _, tool := range cfg.Tools {
			if tool.PermsRequest != "" {
				perms[cfg.Name+"."+tool.Name] = toolPermInfo{
					Perm:         tool.PermsRequest,
					DynamicPerms: tool.DynamicPerms,
				}
			}
		}
	}

	return perms
}

// PluginDir returns the plugin directory path derived from the running binary.
// Layout: $prefix/bin/moxy → $prefix/share/purse-first/moxy
func PluginDir() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving executable: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("resolving executable symlinks: %w", err)
	}
	// self = /nix/store/...-moxy-0.1.0/bin/moxy
	// want = /nix/store/...-moxy-0.1.0/share/purse-first/moxy
	prefix := filepath.Dir(filepath.Dir(self))
	return filepath.Join(prefix, "share", "purse-first", "moxy"), nil
}

// Deprecated: InstallSettingsHook writes hooks to ~/.claude/settings.json for
// the legacy install-mcp path. New installations should use
// install-claude-plugin, which relies on hooks.json auto-discovery instead.
//
// InstallSettingsHook ensures ~/.claude/settings.json contains a PreToolUse
// hook that fires "moxy hook" for all moxy MCP tools.
func InstallSettingsHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving executable: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return fmt.Errorf("resolving executable symlinks: %w", err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	var settings map[string]any

	data, err := os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
			return fmt.Errorf("creating settings dir: %w", err)
		}
		settings = make(map[string]any)
	} else if err != nil {
		return fmt.Errorf("reading settings: %w", err)
	} else {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parsing settings: %w", err)
		}
	}

	moxyPattern := "mcp__moxy__.*"
	hookCommand := fmt.Sprintf("%s hook", self)

	wantEntry := map[string]any{
		"matcher": moxyPattern,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hookCommand,
			},
		},
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
	}

	// Install the same hook entry for both PreToolUse and PermissionRequest.
	for _, eventName := range []string{"PreToolUse", "PermissionRequest"} {
		entries, _ := hooks[eventName].([]any)

		alreadyInstalled := false
		for _, entry := range entries {
			e, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			matcher, _ := e["matcher"].(string)
			if matcher == moxyPattern {
				alreadyInstalled = true
				break
			}
		}

		if !alreadyInstalled {
			entries = append(entries, wantEntry)
			hooks[eventName] = entries
		}
	}

	// SessionEnd fires once at session close and has no tool-name matcher.
	// moxy's hook uses it to rotate the per-session stderr log.
	sessionEndEntry := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": hookCommand,
			},
		},
	}
	sessionEndEntries, _ := hooks["SessionEnd"].([]any)
	sessionEndInstalled := false
	for _, entry := range sessionEndEntries {
		e, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		inner, _ := e["hooks"].([]any)
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := hm["command"].(string); cmd == hookCommand {
				sessionEndInstalled = true
				break
			}
		}
		if sessionEndInstalled {
			break
		}
	}
	if !sessionEndInstalled {
		sessionEndEntries = append(sessionEndEntries, sessionEndEntry)
		hooks["SessionEnd"] = sessionEndEntries
	}

	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}

	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}
