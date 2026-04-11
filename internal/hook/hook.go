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

	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"

	"github.com/amarbel-llc/moxy/internal/native"
)

// hookInput mirrors the unexported type in go-mcp/command.
type hookInput struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
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

// Handle processes a PreToolUse hook invocation. If the tool is a moxy moxin
// tool with a perms-request configured, it writes the corresponding decision.
// Otherwise it delegates to go-mcp's HandleHook for existing deny-redirect logic.
//
// Follows fail-open: any error in permission discovery silently falls through
// to app.HandleHook.
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

	if strings.HasPrefix(hi.ToolName, moxyToolPrefix) {
		if tryPermsDecision(hi.ToolName, w) {
			return nil
		}
	}

	return app.HandleHook(bytes.NewReader(raw), w)
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
	configs, err := native.DiscoverConfigs(os.Getenv("MOXIN_PATH"), native.SystemMoxinDir())
	if err != nil {
		return nil
	}

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

		// Write the pre-tool-use wrapper script.
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

		manifest := hooksManifest{
			Hooks: map[string][]hooksEntry{
				"PreToolUse": {{
					Matcher: moxyToolPrefix + ".*",
					Hooks: []hookEntry{{
						Type:    "command",
						Command: "${CLAUDE_PLUGIN_ROOT}/hooks/pre-tool-use",
						Timeout: 5,
					}},
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

	entries := manifest.Hooks["PreToolUse"]
	if len(entries) == 0 {
		// Add a new PreToolUse entry for moxy tools.
		manifest.Hooks["PreToolUse"] = []hooksEntry{{
			Matcher: moxyPattern,
			Hooks: []hookEntry{{
				Type:    "command",
				Command: "${CLAUDE_PLUGIN_ROOT}/hooks/pre-tool-use",
				Timeout: 5,
			}},
		}}
	} else {
		// Append moxy pattern to the existing matcher if not already present.
		for i, entry := range entries {
			if strings.Contains(entry.Matcher, moxyPattern) {
				return nil // Already expanded.
			}
			entries[i].Matcher = entry.Matcher + "|" + moxyPattern
		}
		manifest.Hooks["PreToolUse"] = entries
	}

	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling hooks.json: %w", err)
	}

	return os.WriteFile(path, append(out, '\n'), 0o644)
}
