# Paved Paths Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Enforce progressive tool disclosure in moxy by requiring agents to traverse moxyfile-defined stages before unlocking later tools.

**Architecture:** Paved-path state lives in-memory on the `Proxy` struct (keyed by session). `ListToolsV1` filters the tool list based on current state; `CallToolV1` checks for stage advancement after each successful call and emits `tools/listChanged`. A new `paved-paths` meta tool handles path listing and selection.

**Tech Stack:** Go, `internal/config` (TOML parsing), `internal/proxy` (proxy core), `zz-tests_bats` (bats integration tests), `go-mcp` server/protocol packages from `amarbel-llc/purse-first`.

**Rollback:** All changes are additive. Moxyfiles without `[[paved-paths]]` entries are unaffected — the feature is a no-op when no paths are configured.

**Related issue:** #178 — `restart` + moxins + session reconnect (parallel work, not a blocker).

---

## Task 1: Add `[[paved-paths]]` config parsing

**Files:**
- Modify: `internal/config/config.go` (add structs; extend `Config`)
- Modify: `internal/config/config_test.go` (new test)

**Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestParsePavedPaths(t *testing.T) {
	input := `
[[paved-paths]]
name = "onboarding"
description = "Learn the repo before making changes"

  [[paved-paths.stages]]
  label = "orient"
  tools = ["folio.read", "folio.glob"]

  [[paved-paths.stages]]
  label = "edit"
  tools = ["folio.write", "grit.commit"]
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PavedPaths) != 1 {
		t.Fatalf("expected 1 paved path, got %d", len(cfg.PavedPaths))
	}
	p := cfg.PavedPaths[0]
	if p.Name != "onboarding" {
		t.Errorf("name: got %q, want %q", p.Name, "onboarding")
	}
	if len(p.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(p.Stages))
	}
	if p.Stages[0].Label != "orient" {
		t.Errorf("stage 0 label: got %q", p.Stages[0].Label)
	}
	if len(p.Stages[0].Tools) != 2 {
		t.Errorf("stage 0 tools: got %v", p.Stages[0].Tools)
	}
}
```

**Step 2: Run to verify it fails**

```sh
go test ./internal/config/... -v -run TestParsePavedPaths
```
Expected: compile error — `cfg.PavedPaths` undefined.

**Step 3: Add structs to `internal/config/config.go`**

Add after the existing `AnnotationFilter` struct:

```go
type PavedPathStage struct {
	Label string   `toml:"label"`
	Tools []string `toml:"tools"`
}

type PavedPathConfig struct {
	Name        string           `toml:"name"`
	Description string           `toml:"description"`
	Stages      []PavedPathStage `toml:"stages"`
}
```

Add `PavedPaths []PavedPathConfig \`toml:"paved-paths"\`` to the `Config` struct (after `DisableMoxins`).

**Step 4: Run to verify it passes**

```sh
go test ./internal/config/... -v -run TestParsePavedPaths
```
Expected: PASS.

**Step 5: Commit**

```sh
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add PavedPathConfig parsing"
```

---

## Task 2: Wire `tools/listChanged` notification emission

**Files:**
- Modify: `internal/proxy/proxy.go` (add `notifyToolsChanged` helper)

**Step 1: Write the failing test**

Add to `internal/proxy/proxy_test.go` (or create if it doesn't exist):

```go
func TestNotifyToolsChangedCallsNotifier(t *testing.T) {
	var called int
	p := &Proxy{
		notifier: func(msg *jsonrpc.Message) error {
			called++
			return nil
		},
	}
	p.notifyToolsChanged()
	if called != 1 {
		t.Errorf("expected notifier called 1 time, got %d", called)
	}
}

func TestNotifyToolsChangedNoNotifier(t *testing.T) {
	p := &Proxy{}
	// Must not panic when notifier is nil
	p.notifyToolsChanged()
}
```

**Step 2: Run to verify it fails**

```sh
go test ./internal/proxy/... -v -run TestNotifyToolsChanged
```
Expected: compile error — `notifyToolsChanged` undefined.

**Step 3: Add `notifyToolsChanged` to `internal/proxy/proxy.go`**

Add after the existing `ForwardNotification` method (around line 138):

```go
func (p *Proxy) notifyToolsChanged() {
	if p.notifier == nil {
		return
	}
	msg := &jsonrpc.Message{
		Method: "notifications/tools/list_changed",
	}
	_ = p.notifier(msg)
}
```

Check the exact JSON-RPC notification method name against the go-mcp protocol package — it may be `"notifications/tools/list_changed"` or a constant. Use the constant if one exists.

**Step 4: Run to verify it passes**

```sh
go test ./internal/proxy/... -v -run TestNotifyToolsChanged
```
Expected: PASS.

**Step 5: Commit**

```sh
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "feat(proxy): add notifyToolsChanged helper"
```

---

## Task 3: Add paved-path state to `Proxy`

**Files:**
- Modify: `internal/proxy/proxy.go` (add state struct + field; add accessor)

**Step 1: Write the failing test**

```go
func TestPavedPathStateInitial(t *testing.T) {
	p := &Proxy{}
	if p.pavedPathState != nil {
		t.Error("expected nil paved path state initially")
	}
}
```

**Step 2: Run to verify it fails**

```sh
go test ./internal/proxy/... -v -run TestPavedPathStateInitial
```
Expected: compile error — `pavedPathState` undefined.

**Step 3: Add state struct and field**

Add to `internal/proxy/proxy.go`:

```go
type pavedPathState struct {
	SelectedPath string
	CurrentStage int
	CalledTools  map[string]bool
	Complete     bool
}
```

Add `pavedPathState *pavedPathState` field to the `Proxy` struct (after `builtinTools`). The `mu` field already protects it — no new lock needed.

**Step 4: Run to verify it passes**

```sh
go test ./internal/proxy/... -v -run TestPavedPathStateInitial
```
Expected: PASS.

**Step 5: Commit**

```sh
git add internal/proxy/proxy.go internal/proxy/proxy_test.go
git commit -m "feat(proxy): add pavedPathState field to Proxy"
```

---

## Task 4: Pass paved-path config to `Proxy` and filter `ListToolsV1`

**Files:**
- Modify: `internal/proxy/proxy.go` (add `pavedPaths` config field; filter logic in `ListToolsV1`)
- Modify: `cmd/moxy/main.go` (pass config to proxy after construction)

**Step 1: Write the failing test**

This tests the filtering behavior. You'll need to construct a minimal `Proxy` with `pavedPaths` set and no `pavedPathState` (meaning path not yet selected), then call `ListToolsV1` and verify only the `paved-paths` tool would be returned (tools from children are absent).

Since `ListToolsV1` calls children, use a mock or check the filtering predicate directly:

```go
func TestPavedPathsActiveFiltersTools(t *testing.T) {
	paths := []config.PavedPathConfig{
		{
			Name: "onboarding",
			Stages: []config.PavedPathStage{
				{Label: "orient", Tools: []string{"folio.read"}},
			},
		},
	}
	p := &Proxy{pavedPaths: paths}
	// No state selected yet — tools from children should be suppressed
	if !p.pavedPathsActive() {
		t.Error("expected pavedPathsActive true when paths configured")
	}
	if p.pavedPathToolAllowed("folio.read") {
		t.Error("folio.read should not be allowed before path selection")
	}
}

func TestNoPavedPathsNotActive(t *testing.T) {
	p := &Proxy{}
	if p.pavedPathsActive() {
		t.Error("expected pavedPathsActive false when no paths configured")
	}
}
```

**Step 2: Run to verify it fails**

```sh
go test ./internal/proxy/... -v -run TestPavedPaths
```

**Step 3: Add config field and helper methods**

Add `pavedPaths []config.PavedPathConfig` to `Proxy` struct.

Add methods:

```go
// pavedPathsActive returns true if paved paths are configured (feature is on).
func (p *Proxy) pavedPathsActive() bool {
	return len(p.pavedPaths) > 0
}

// pavedPathToolAllowed returns true if the named tool may appear in tools/list.
// When paved paths are active and no path is selected, only "paved-paths" is allowed.
// When a path is selected, only tools in the current stage are allowed.
// When the path is complete, all tools are allowed.
func (p *Proxy) pavedPathToolAllowed(name string) bool {
	if !p.pavedPathsActive() {
		return true
	}
	if name == "paved-paths" {
		return true
	}
	if p.pavedPathState == nil {
		return false // no path selected yet
	}
	if p.pavedPathState.Complete {
		return true
	}
	stage := p.pavedPaths[stageIndex(p.pavedPaths, p.pavedPathState.SelectedPath, p.pavedPathState.CurrentStage)]
	for _, t := range stage.Tools {
		if t == name {
			return true
		}
	}
	return false
}
```

You'll need a `stageIndex` helper or inline the lookup. Keep it simple — iterate `p.pavedPaths` to find the path by name, then index into `Stages`.

**Step 4: Filter in `ListToolsV1`**

In `ListToolsV1` (around line 488, after annotation filter), add:

```go
if !p.pavedPathToolAllowed(serverName + "." + tool.Name) {
    continue
}
```

Note: the prefixing happens *after* the filter in the current code — adjust to filter on the prefixed name or factor accordingly.

**Step 5: Pass config in `cmd/moxy/main.go`**

After `p.SetBuiltinTools(builtinRegistry)` (line 494), add:

```go
p.SetPavedPaths(cfg.PavedPaths)
```

Add `SetPavedPaths` method to `Proxy`:

```go
func (p *Proxy) SetPavedPaths(paths []config.PavedPathConfig) {
	p.pavedPaths = paths
}
```

**Step 6: Run to verify tests pass**

```sh
go test ./internal/proxy/... -v -run TestPavedPaths
go test ./internal/config/...
```

**Step 7: Commit**

```sh
git add internal/proxy/proxy.go cmd/moxy/main.go
git commit -m "feat(proxy): filter tools/list based on paved-path state"
```

---

## Task 5: Implement `paved-paths` meta tool

**Files:**
- Modify: `internal/proxy/proxy.go` (add `handlePavedPaths`; wire into `CallToolV1` and `ListToolsV1`)
- Modify: `cmd/moxy/main.go` (register tool schema in builtin registry, or add as a direct dispatch like `restart`)

**Step 1: Write the failing test**

```go
func TestPavedPathsToolListsPaths(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{
			{Name: "onboarding", Description: "Get started"},
		},
	}
	result, err := p.handlePavedPaths(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) == 0 {
		t.Error("expected non-empty content")
	}
	// Result text should mention the path name
	text := result.Content[0].Text
	if !strings.Contains(text, "onboarding") {
		t.Errorf("expected 'onboarding' in result, got: %s", text)
	}
}

func TestPavedPathsToolSelectsPath(t *testing.T) {
	var notified bool
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{
			{
				Name: "onboarding",
				Stages: []config.PavedPathStage{
					{Label: "orient", Tools: []string{"folio.read"}},
				},
			},
		},
		notifier: func(*jsonrpc.Message) error {
			notified = true
			return nil
		},
	}
	args := json.RawMessage(`{"select": "onboarding"}`)
	_, err := p.handlePavedPaths(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if p.pavedPathState == nil || p.pavedPathState.SelectedPath != "onboarding" {
		t.Error("expected path to be selected")
	}
	if !notified {
		t.Error("expected tools/listChanged notification")
	}
}
```

**Step 2: Run to verify it fails**

```sh
go test ./internal/proxy/... -v -run TestPavedPathsTool
```

**Step 3: Implement `handlePavedPaths`**

Add to `internal/proxy/proxy.go`:

```go
func (p *Proxy) handlePavedPaths(ctx context.Context, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	var req struct {
		Select string `json:"select"`
	}
	if args != nil {
		_ = json.Unmarshal(args, &req)
	}

	if req.Select != "" {
		// Find path by name
		var found *config.PavedPathConfig
		for i := range p.pavedPaths {
			if p.pavedPaths[i].Name == req.Select {
				found = &p.pavedPaths[i]
				break
			}
		}
		if found == nil {
			return errorResult(fmt.Sprintf("unknown path %q", req.Select)), nil
		}
		p.mu.Lock()
		p.pavedPathState = &pavedPathState{
			SelectedPath: req.Select,
			CurrentStage: 0,
			CalledTools:  make(map[string]bool),
		}
		p.mu.Unlock()
		p.notifyToolsChanged()
		firstStage := found.Stages[0]
		return textResult(fmt.Sprintf(
			"Path %q selected. Stage 1/%d: %q\nTools now available: %s",
			req.Select, len(found.Stages), firstStage.Label,
			strings.Join(firstStage.Tools, ", "),
		)), nil
	}

	// No select arg — return status
	if p.pavedPathState != nil {
		state := p.pavedPathState
		var path *config.PavedPathConfig
		for i := range p.pavedPaths {
			if p.pavedPaths[i].Name == state.SelectedPath {
				path = &p.pavedPaths[i]
				break
			}
		}
		if state.Complete {
			return textResult(fmt.Sprintf("Path %q complete. All tools unlocked.", state.SelectedPath)), nil
		}
		stage := path.Stages[state.CurrentStage]
		var needed []string
		for _, t := range stage.Tools {
			if !state.CalledTools[t] {
				needed = append(needed, t)
			}
		}
		return textResult(fmt.Sprintf(
			"Path: %q  Stage %d/%d: %q\nStill needed: %s",
			state.SelectedPath, state.CurrentStage+1, len(path.Stages),
			stage.Label, strings.Join(needed, ", "),
		)), nil
	}

	// No path selected — list available paths
	var lines []string
	for _, path := range p.pavedPaths {
		lines = append(lines, fmt.Sprintf("- %s: %s", path.Name, path.Description))
	}
	return textResult("Available paths:\n" + strings.Join(lines, "\n") +
		"\n\nCall paved-paths with {\"select\": \"<name>\"} to begin."), nil
}
```

Add small helpers if they don't exist:
```go
func textResult(s string) *protocol.ToolCallResultV1 {
	return &protocol.ToolCallResultV1{Content: []protocol.ContentV1{{Type: "text", Text: s}}}
}
func errorResult(s string) *protocol.ToolCallResultV1 {
	return &protocol.ToolCallResultV1{IsError: true, Content: []protocol.ContentV1{{Type: "text", Text: s}}}
}
```

(Check if these helpers already exist in the file before adding.)

**Step 4: Wire into `CallToolV1`**

In `CallToolV1` (after the `exec-mcp` check), add:

```go
if name == "paved-paths" {
	return p.handlePavedPaths(ctx, args)
}
```

**Step 5: Run to verify tests pass**

```sh
go test ./internal/proxy/... -v -run TestPavedPathsTool
```

**Step 6: Commit**

```sh
git add internal/proxy/proxy.go
git commit -m "feat(proxy): implement paved-paths meta tool handler"
```

---

## Task 6: Stage advancement in `CallToolV1`

**Files:**
- Modify: `internal/proxy/proxy.go` (advance stage after successful tool call)

**Step 1: Write the failing test**

```go
func TestStageAdvancesOnToolCall(t *testing.T) {
	var notifyCount int
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{
			{
				Name: "onboarding",
				Stages: []config.PavedPathStage{
					{Label: "orient", Tools: []string{"folio.read"}},
					{Label: "edit", Tools: []string{"folio.write"}},
				},
			},
		},
		pavedPathState: &pavedPathState{
			SelectedPath: "onboarding",
			CurrentStage: 0,
			CalledTools:  make(map[string]bool),
		},
		notifier: func(*jsonrpc.Message) error {
			notifyCount++
			return nil
		},
	}
	p.maybeAdvanceStage("folio.read")
	if p.pavedPathState.CurrentStage != 1 {
		t.Errorf("expected stage 1, got %d", p.pavedPathState.CurrentStage)
	}
	if notifyCount != 1 {
		t.Errorf("expected 1 notification, got %d", notifyCount)
	}
}

func TestPathCompletesAfterLastStage(t *testing.T) {
	p := &Proxy{
		pavedPaths: []config.PavedPathConfig{
			{
				Name: "onboarding",
				Stages: []config.PavedPathStage{
					{Label: "orient", Tools: []string{"folio.read"}},
				},
			},
		},
		pavedPathState: &pavedPathState{
			SelectedPath: "onboarding",
			CurrentStage: 0,
			CalledTools:  make(map[string]bool),
		},
		notifier: func(*jsonrpc.Message) error { return nil },
	}
	p.maybeAdvanceStage("folio.read")
	if !p.pavedPathState.Complete {
		t.Error("expected path to be complete after last stage")
	}
}
```

**Step 2: Run to verify it fails**

```sh
go test ./internal/proxy/... -v -run TestStageAdvances
go test ./internal/proxy/... -v -run TestPathCompletes
```

**Step 3: Implement `maybeAdvanceStage`**

Add to `internal/proxy/proxy.go`:

```go
func (p *Proxy) maybeAdvanceStage(toolName string) {
	if p.pavedPathState == nil || p.pavedPathState.Complete {
		return
	}
	// Find the current path config
	var path *config.PavedPathConfig
	for i := range p.pavedPaths {
		if p.pavedPaths[i].Name == p.pavedPathState.SelectedPath {
			path = &p.pavedPaths[i]
			break
		}
	}
	if path == nil {
		return
	}
	stage := path.Stages[p.pavedPathState.CurrentStage]
	// Check if toolName is in this stage
	for _, t := range stage.Tools {
		if t == toolName {
			p.mu.Lock()
			p.pavedPathState.CalledTools[toolName] = true
			p.pavedPathState.CurrentStage++
			if p.pavedPathState.CurrentStage >= len(path.Stages) {
				p.pavedPathState.Complete = true
			}
			p.mu.Unlock()
			p.notifyToolsChanged()
			return
		}
	}
}
```

**Step 4: Call `maybeAdvanceStage` in `CallToolV1`**

After a successful child tool call returns (around where the result is assembled), add:

```go
p.maybeAdvanceStage(name) // name is the namespaced tool name e.g. "folio.read"
```

Place this *after* the result is received and before returning it, and only when `err == nil && !result.IsError`.

**Step 5: Run to verify tests pass**

```sh
go test ./internal/proxy/... -v -run TestStageAdvances
go test ./internal/proxy/... -v -run TestPathCompletes
go test ./internal/proxy/...
```

**Step 6: Commit**

```sh
git add internal/proxy/proxy.go
git commit -m "feat(proxy): advance paved-path stage after tool call"
```

---

## Task 7: Bats integration tests

**Files:**
- Create: `zz-tests_bats/paved_paths.bats`

**Step 1: Write tests**

Pattern: use `run_moxy_mcp` / `run_moxy_mcp_two` from `common.bash`. Write a temp moxyfile with `[[paved-paths]]` and assert on tool list contents.

```bash
#!/usr/bin/env bats
# shellcheck disable=SC2030,SC2031

load common

setup() {
  setup_home
  # Write a moxyfile with a paved path using only the echo moxin (available in test env)
  cat > "$HOME/.config/moxy/moxyfile" <<'EOF'
[[paved-paths]]
name = "test-path"
description = "A test paved path"

  [[paved-paths.stages]]
  label = "first"
  tools = ["folio.read"]
EOF
}

@test "before path selection, only paved-paths tool is listed" {
  run_moxy_mcp '{"method":"tools/list","params":{}}'
  assert_output --partial '"name":"paved-paths"'
  refute_output --partial '"name":"folio.read"'
}

@test "paved-paths tool lists available paths" {
  run_moxy_mcp '{"method":"tools/call","params":{"name":"paved-paths","arguments":{}}}'
  assert_output --partial 'test-path'
  assert_output --partial 'A test paved path'
}

@test "selecting a path unlocks stage tools" {
  run_moxy_mcp_two \
    '{"method":"tools/call","params":{"name":"paved-paths","arguments":{"select":"test-path"}}}' \
    '{"method":"tools/list","params":{}}'
  # Second call result should contain folio.read
  assert_output --partial '"name":"folio.read"'
}
```

**Step 2: Run tests**

```sh
just test-bats-file paved_paths.bats
```

Expected: all pass. Debug failures using `just test-bats-file paved_paths.bats --print-output-on-failure`.

**Step 3: Commit**

```sh
git add zz-tests_bats/paved_paths.bats
git commit -m "test(bats): paved-paths integration tests"
```

---

## Task 8: Smoke test end-to-end

**Step 1: Build**

```sh
just build-go
```

**Step 2: Run all tests**

```sh
just test
```

Expected: all green.

**Step 3: Manual smoke test**

Write a temp moxyfile with a `[[paved-paths]]` entry. Run `moxy` against it and use `tools/list` + `tools/call paved-paths` manually (or via `run_moxy_mcp`) to verify:

1. Before selection: only `paved-paths` in tool list
2. After `paved-paths {"select": "..."}`: stage tools appear, `tools/listChanged` notification emitted
3. After calling a stage tool: next stage tools appear

**Step 4: Commit any fixes, then finalize**

```sh
git add -A
git commit -m "fix: paved-paths smoke test fixes" # if needed
```

---

## Open Questions (captured from design; resolve in FDR)

- Stage advancement: any tool vs. all tools in stage
- Path completion: unlock all vs. only last stage
- Multiple paths: show all vs. first
- Always-available tools (future option)
- Disk persistence to `~/.local/state/moxy/` (follow-up task)
- Human-in-the-loop elicitation (future enhancement)
- See issue #178 for `restart` + moxin + session reconnect gaps
