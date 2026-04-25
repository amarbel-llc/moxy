// Added for moxy POC dynamic-perms
//
// Self-asserting POC driver for the dynamic perms-request feature.
// Exits 0 with "PASS" on success, 1 with "FAIL: ..." on any failure.
// All constants are hardcoded — no flags, no env vars, no config files.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/amarbel-llc/moxy/internal/native"
)

const (
	moxinName = "pocfolio"
	toolName  = "write"

	// timeoutForUnmapped is shorter than the slow predicate's sleep so the
	// timeout sub-test fires the deadline path. The slow predicate uses 5s.
	pocTimeoutMS = 2000
)

// moxinMetaTOML is the _moxin.toml of the hand-built fixture moxin.
const moxinMetaTOML = `schema = 1
name = "pocfolio"
description = "POC fixture for dynamic-perms"
`

// toolTOML is the per-tool fixture. perms-request = "dynamic" with a
// [dynamic-perms] block whose command + arg-order mirror the main tool.
const toolTOML = `schema = 3
description = "Write a file (POC)"
command = "bash"
args = ["-c", "echo wrote"]
arg-order = ["file_path"]
stdin-param = "content"
perms-request = "dynamic"

[dynamic-perms]
command = "bash"
args = ["-c", "[[ \"$1\" == \"$PWD\"* ]]"]
arg-order = ["file_path"]
timeout-ms = 2000

[input]
type = "object"
required = ["file_path", "content"]

[input.properties.file_path]
type = "string"
description = "Target path"

[input.properties.content]
type = "string"
description = "Content to write"
`

// hookOutput mirrors internal/hook/hook.go's emit shape so we can render the
// JSON moxy would actually return for a deny decision and eyeball it.
type hookOutput struct {
	HookSpecificOutput hookDecision `json:"hookSpecificOutput"`
}

type hookDecision struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PASS")
}

func run() error {
	if err := assertParser(); err != nil {
		return fmt.Errorf("parser: %w", err)
	}
	if err := assertRejectsBadFixtures(); err != nil {
		return fmt.Errorf("validator: %w", err)
	}
	if err := assertExecutor(); err != nil {
		return fmt.Errorf("executor: %w", err)
	}
	return nil
}

// assertParser writes the fixture moxin, parses it, and confirms ToolSpec
// is populated correctly.
func assertParser() error {
	dir, err := writeFixtureMoxin()
	if err != nil {
		return fmt.Errorf("writing fixture: %w", err)
	}

	cfg, err := native.ParseMoxinDir(dir)
	if err != nil {
		return fmt.Errorf("parsing fixture: %w", err)
	}

	if cfg.Name != moxinName {
		return fmt.Errorf("moxin name: got %q want %q", cfg.Name, moxinName)
	}
	if len(cfg.Tools) != 1 {
		return fmt.Errorf("tool count: got %d want 1", len(cfg.Tools))
	}

	tool := cfg.Tools[0]
	if tool.Name != toolName {
		return fmt.Errorf("tool name: got %q want %q", tool.Name, toolName)
	}
	if tool.PermsRequest != native.PermsDynamic {
		return fmt.Errorf("perms-request: got %q want %q", tool.PermsRequest, native.PermsDynamic)
	}
	if tool.DynamicPerms == nil {
		return fmt.Errorf("DynamicPerms: nil, want populated spec")
	}
	if tool.DynamicPerms.Command != "bash" {
		return fmt.Errorf("DynamicPerms.Command: got %q want %q", tool.DynamicPerms.Command, "bash")
	}
	wantArgs := []string{"-c", "[[ \"$1\" == \"$PWD\"* ]]"}
	if !reflect.DeepEqual(tool.DynamicPerms.Args, wantArgs) {
		return fmt.Errorf("DynamicPerms.Args: got %v want %v", tool.DynamicPerms.Args, wantArgs)
	}
	wantOrder := []string{"file_path"}
	if !reflect.DeepEqual(tool.DynamicPerms.ArgOrder, wantOrder) {
		return fmt.Errorf("DynamicPerms.ArgOrder: got %v want %v", tool.DynamicPerms.ArgOrder, wantOrder)
	}
	if tool.DynamicPerms.TimeoutMS != 2000 {
		return fmt.Errorf("DynamicPerms.TimeoutMS: got %d want 2000", tool.DynamicPerms.TimeoutMS)
	}
	return nil
}

// assertRejectsBadFixtures verifies the validator rejects the three contract
// violations: dynamic without a block, block without dynamic, missing command.
func assertRejectsBadFixtures() error {
	cases := []struct {
		name string
		tool string
		want string
	}{
		{
			name: "dynamic without [dynamic-perms]",
			tool: `schema = 3
command = "bash"
args = ["-c", "true"]
perms-request = "dynamic"
`,
			want: "[dynamic-perms] block",
		},
		{
			name: "[dynamic-perms] without dynamic",
			tool: `schema = 3
command = "bash"
args = ["-c", "true"]
perms-request = "always-allow"

[dynamic-perms]
command = "bash"
`,
			want: "only valid with perms-request",
		},
		{
			name: "[dynamic-perms] missing command",
			tool: `schema = 3
command = "bash"
args = ["-c", "true"]
perms-request = "dynamic"

[dynamic-perms]
args = ["-c", "true"]
`,
			want: "command is required",
		},
	}

	for _, tc := range cases {
		dir, err := writeBareFixture(tc.tool)
		if err != nil {
			return fmt.Errorf("%s: setup: %w", tc.name, err)
		}
		_, err = native.ParseMoxinDir(dir)
		if err == nil {
			return fmt.Errorf("%s: parser accepted invalid fixture", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			return fmt.Errorf("%s: error %q does not contain %q", tc.name, err.Error(), tc.want)
		}
	}
	return nil
}

// assertExecutor exercises the 5 exit-code paths against an in-process call
// to native.EvalDynamicPerms with hand-built specs. Path under CWD → allow,
// outside CWD → ask, forbidden path → deny, sleeping → ask+timeout reason,
// unmapped exit → ask+unmapped reason.
func assertExecutor() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	insidePath := filepath.Join(cwd, "subdir/file.txt")
	outsidePath := "/tmp/elsewhere.txt"
	forbiddenPath := "/etc/passwd"

	predicate := `set -e
case "$1" in
  /etc/*) echo "forbidden prefix" >&2; exit 2 ;;
esac
[[ "$1" == "$PWD"* ]] && { echo "under cwd"; exit 0; }
echo "outside cwd" >&2
exit 1
`
	spec := &native.DynamicPermsSpec{
		Command:   "bash",
		Args:      []string{"-c", predicate, "predicate"},
		ArgOrder:  []string{"file_path"},
		TimeoutMS: pocTimeoutMS,
	}

	cases := []struct {
		name           string
		spec           *native.DynamicPermsSpec
		args           string
		wantDecision   native.DynamicPermsDecision
		reasonContains string
	}{
		{
			name:           "path under cwd → allow",
			spec:           spec,
			args:           argsJSON(insidePath),
			wantDecision:   native.DynPermsAllow,
			reasonContains: "exit 0",
		},
		{
			name:           "path outside cwd → ask",
			spec:           spec,
			args:           argsJSON(outsidePath),
			wantDecision:   native.DynPermsAsk,
			reasonContains: "exit 1",
		},
		{
			name:           "forbidden path → deny",
			spec:           spec,
			args:           argsJSON(forbiddenPath),
			wantDecision:   native.DynPermsDeny,
			reasonContains: "forbidden prefix",
		},
		{
			name: "predicate sleeps past timeout → ask",
			spec: &native.DynamicPermsSpec{
				Command:   "bash",
				Args:      []string{"-c", "sleep 5"},
				ArgOrder:  []string{"file_path"},
				TimeoutMS: pocTimeoutMS,
			},
			args:           argsJSON(insidePath),
			wantDecision:   native.DynPermsAsk,
			reasonContains: "timed out",
		},
		{
			name: "predicate exits 99 (unmapped) → ask",
			spec: &native.DynamicPermsSpec{
				Command:   "bash",
				Args:      []string{"-c", "exit 99"},
				ArgOrder:  []string{"file_path"},
				TimeoutMS: pocTimeoutMS,
			},
			args:           argsJSON(insidePath),
			wantDecision:   native.DynPermsAsk,
			reasonContains: "unmapped code 99",
		},
	}

	for _, tc := range cases {
		decision, reason := native.EvalDynamicPerms(
			context.Background(),
			tc.spec,
			nil,
			json.RawMessage(tc.args),
		)
		if decision != tc.wantDecision {
			return fmt.Errorf("%s: decision=%q reason=%q, want decision=%q",
				tc.name, decision, reason, tc.wantDecision)
		}
		if !strings.Contains(reason, tc.reasonContains) {
			return fmt.Errorf("%s: reason=%q does not contain %q",
				tc.name, reason, tc.reasonContains)
		}
		fmt.Printf("  ok: %s → decision=%q reason=%q\n", tc.name, decision, reason)
	}

	// Eyeball check: render the deny-case JSON exactly as moxy's hook would
	// emit it so we can confirm Claude Code's protocol accepts "deny".
	denyDecision, denyReason := native.EvalDynamicPerms(
		context.Background(),
		spec,
		nil,
		json.RawMessage(argsJSON(forbiddenPath)),
	)
	out := hookOutput{
		HookSpecificOutput: hookDecision{
			HookEventName:            "PreToolUse",
			PermissionDecision:       string(denyDecision),
			PermissionDecisionReason: denyReason,
		},
	}
	js, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshaling deny case: %w", err)
	}
	fmt.Printf("\nDENY-CASE HOOK JSON (eyeball check):\n  %s\n", js)

	return nil
}

func argsJSON(path string) string {
	js, _ := json.Marshal(map[string]string{
		"file_path": path,
		"content":   "ignored by predicate",
	})
	return string(js)
}

func writeFixtureMoxin() (string, error) {
	dir, err := os.MkdirTemp("", "moxy-poc-dynperms-*")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "_moxin.toml"), []byte(moxinMetaTOML), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, toolName+".toml"), []byte(toolTOML), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

func writeBareFixture(tool string) (string, error) {
	dir, err := os.MkdirTemp("", "moxy-poc-dynperms-bad-*")
	if err != nil {
		return "", err
	}
	meta := `schema = 1
name = "badfixture"
`
	if err := os.WriteFile(filepath.Join(dir, "_moxin.toml"), []byte(meta), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "t.toml"), []byte(tool), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}
