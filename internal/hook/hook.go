package hook

import (
	"bytes"
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
)

var (
	hookLog    *log.Logger
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

const moxyToolPrefix = "mcp__moxy__"

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

	// Fire-hose: log every hook event to per-session JSONL.
	logHookEvent(json.RawMessage(raw), hi)

	switch hi.HookEventName {
	case "PermissionRequest":
		// Return empty output — Claude Code shows the normal permission dialog.
		return nil

	default:
		// PreToolUse (and any future event types): existing behavior.
		if strings.HasPrefix(hi.ToolName, moxyToolPrefix) {
			parsed, ok := parseNativeToolName(hi.ToolName)
			debugHook("  parsed=%q ok=%v", parsed, ok)
			if tryPermsDecision(hi.ToolName, w) {
				debugHook("  decision: allowed")
				return nil
			}
			debugHook("  decision: fall-through")
		}

		return app.HandleHook(bytes.NewReader(raw), w)
	}
}

// tryPermsDecision checks the tool's perms-request in moxin configs and writes
// the corresponding hook decision. Returns true if it wrote a decision, false
// to fall through to the client.
func tryPermsDecision(toolName string, w io.Writer) bool {
	serverTool, ok := parseNativeToolName(toolName)
	if !ok {
		return false
	}

	perms := discoverPermissions()
	debugHook("  perms map has %d entries, looking up %q", len(perms), serverTool)
	perm, exists := perms[serverTool]
	if !exists {
		return false // delegate-to-client: fall through
	}

	var decision, reason string
	switch perm {
	case native.PermsAlwaysAllow:
		decision = "allow"
		reason = "always-allow by moxin config"
	case native.PermsEachUse:
		decision = "ask"
		reason = "each-use: requires explicit approval"
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

// parseNativeToolName converts "mcp__moxy__folio_read" to "folio.read".
// Server names may contain hyphens but not underscores or dots, so the first
// underscore after the prefix separates server name from tool name.
func parseNativeToolName(toolName string) (string, bool) {
	suffix := strings.TrimPrefix(toolName, moxyToolPrefix)
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

// discoverPermissions loads moxin configs and returns a map of
// "server.tool" names to their perms-request values. Only tools with
// an explicit perms-request are included.
func discoverPermissions() map[string]native.PermsRequest {
	moxinPath := os.Getenv("MOXIN_PATH")
	systemDir := native.SystemMoxinDir()
	debugHook("  discoverPermissions: MOXIN_PATH=%q systemDir=%q", moxinPath, systemDir)
	configs, err := native.DiscoverConfigs(moxinPath, systemDir)
	if err != nil {
		debugHook("  discoverPermissions error: %v", err)
		return nil
	}
	debugHook("  discoverPermissions: found %d configs", len(configs))

	perms := make(map[string]native.PermsRequest)
	for _, cfg := range configs {
		for _, tool := range cfg.Tools {
			if tool.PermsRequest != "" {
				perms[cfg.Name+"."+tool.Name] = tool.PermsRequest
			}
		}
	}

	return perms
}

// InstallSettingsHook ensures ~/.claude/settings.json contains a PreToolUse
// hook that fires "moxy hook" for all moxy MCP tools. This is called by
// install-mcp so that auto-allow works without a separate plugin installation.
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

	moxyPattern := moxyToolPrefix + ".*"
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

	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}

	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// hooksManifest mirrors the hooks.json structure for matcher expansion.
type hooksManifest struct {
	Hooks map[string][]hooksEntry `json:"hooks"`
}

type hooksEntry struct {
	Matcher string      `json:"matcher"`
	Hooks   []hookEntry `json:"hooks"`
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// ExpandHooksMatcher reads the generated hooks.json and appends the moxy MCP
// tool pattern to the PreToolUse matcher so the hook fires for moxy's own
// tools (not just built-in Claude tools like Bash/Read/Glob).
//
// dir is the output directory passed to generate-plugin (e.g. ".").
// appName is the app name (e.g. "moxy").
func ExpandHooksMatcher(dir, appName string) error {
	path := filepath.Join(dir, appName, "hooks", "hooks.json")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// No hooks.json means no tool mappings from go-mcp. Create the
		// hooks directory, hooks.json, and pre-tool-use wrapper script.
		hooksDir := filepath.Dir(path)
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			return fmt.Errorf("creating hooks dir: %w", err)
		}

		// Write the hook wrapper script (shared by PreToolUse and PermissionRequest).
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolving executable: %w", err)
		}
		self, err = filepath.EvalSymlinks(self)
		if err != nil {
			return fmt.Errorf("resolving executable symlinks: %w", err)
		}
		script := fmt.Sprintf("#!/bin/sh\nexec '%s' hook\n", self)
		if err := os.WriteFile(filepath.Join(hooksDir, "pre-tool-use"), []byte(script), 0o755); err != nil {
			return fmt.Errorf("writing pre-tool-use: %w", err)
		}

		he := hookEntry{
			Type:    "command",
			Command: "${CLAUDE_PLUGIN_ROOT}/hooks/pre-tool-use",
			Timeout: 5,
		}
		manifest := hooksManifest{
			Hooks: map[string][]hooksEntry{
				"PreToolUse": {{
					Matcher: moxyToolPrefix + ".*",
					Hooks:   []hookEntry{he},
				}},
				"PermissionRequest": {{
					Matcher: moxyToolPrefix + ".*",
					Hooks:   []hookEntry{he},
				}},
			},
		}
		out, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling hooks.json: %w", err)
		}
		return os.WriteFile(path, append(out, '\n'), 0o644)
	}
	if err != nil {
		return fmt.Errorf("reading hooks.json: %w", err)
	}

	var manifest hooksManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing hooks.json: %w", err)
	}

	moxyPattern := moxyToolPrefix + ".*"

	// Ensure both PreToolUse and PermissionRequest have the moxy pattern.
	for _, eventName := range []string{"PreToolUse", "PermissionRequest"} {
		entries := manifest.Hooks[eventName]
		if len(entries) == 0 {
			manifest.Hooks[eventName] = []hooksEntry{{
				Matcher: moxyPattern,
				Hooks: []hookEntry{{
					Type:    "command",
					Command: "${CLAUDE_PLUGIN_ROOT}/hooks/pre-tool-use",
					Timeout: 5,
				}},
			}}
		} else {
			alreadyPresent := false
			for _, entry := range entries {
				if strings.Contains(entry.Matcher, moxyPattern) {
					alreadyPresent = true
					break
				}
			}
			if !alreadyPresent {
				for i, entry := range entries {
					entries[i].Matcher = entry.Matcher + "|" + moxyPattern
				}
				manifest.Hooks[eventName] = entries
			}
		}
	}

	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling hooks.json: %w", err)
	}

	return os.WriteFile(path, append(out, '\n'), 0o644)
}
