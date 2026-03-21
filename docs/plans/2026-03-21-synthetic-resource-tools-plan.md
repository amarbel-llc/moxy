# Synthetic Resource Tools + Snob-Case Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Convert tool/prompt names to snob-case and auto-generate
`resource_read`/`resource_templates` tools for resource-capable child servers.

**Architecture:** Snob-case conversion happens at the proxy layer ---
`ListToolsV1`/`ListPromptsV1` convert hyphens to underscores in child names,
`CallToolV1`/`GetPromptV1` reverse the conversion before forwarding. Synthetic
resource tools are injected into `ListToolsV1` for children with resource
capabilities and intercepted in `CallToolV1` to dispatch to `resources/read` and
`resources/templates/list` RPC calls. A `ResourceTools *bool` config field
provides opt-out.

**Tech Stack:** Go, TOML (tommy), bats integration tests

**Rollback:** Revert the commits. Snob-case is a breaking rename --- no
dual-architecture.

--------------------------------------------------------------------------------

### Task 1: Add `ResourceTools` config field

**Promotion criteria:** N/A

**Files:** - Modify: `internal/config/config.go:19-24` - Modify:
`internal/config/config.go:127-132` (Parse function) - Test:
`internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

``` go
func TestParseResourceTools(t *testing.T) {
    input := `
[[servers]]
name = "grit"
command = "grit"
resource_tools = false
`
    cfg, err := Parse([]byte(input))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cfg.Servers[0].ResourceTools == nil || *cfg.Servers[0].ResourceTools != false {
        t.Error("expected resource_tools = false")
    }
}

func TestParseResourceToolsDefault(t *testing.T) {
    input := `
[[servers]]
name = "grit"
command = "grit"
`
    cfg, err := Parse([]byte(input))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cfg.Servers[0].ResourceTools != nil {
        t.Error("expected resource_tools = nil (absent)")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run TestParseResourceTools` Expected:
FAIL --- `ResourceTools` field doesn't exist on `ServerConfig`

**Step 3: Write minimal implementation**

In `internal/config/config.go`, add `ResourceTools` field to `ServerConfig`:

``` go
type ServerConfig struct {
    Name           string            `toml:"name"`
    Command        Command           `toml:"command"`
    Annotations    *AnnotationFilter `toml:"annotations"`
    Paginate       bool              `toml:"paginate"`
    ResourceTools  *bool             `toml:"resource_tools"`
}
```

In the `Parse` function, after the `paginate` parsing (line \~131), add:

``` go
if rt, err := document.GetFromContainer[bool](doc, node, "resource_tools"); err == nil {
    cfg.Servers[i].ResourceTools = &rt
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run TestParseResourceTools` Expected:
PASS

**Step 5: Commit**

    feat(config): add resource_tools field to ServerConfig

--------------------------------------------------------------------------------

### Task 2: Add snob-case helper and proxy conversion for tools

**Promotion criteria:** N/A

**Files:** - Modify: `internal/proxy/proxy.go:120-129` (ListToolsV1 tool name
prefixing) - Modify: `internal/proxy/proxy.go:149-188` (CallToolV1 tool name
dispatch)

**Step 1: Write the failing test**

Create `internal/proxy/snobcase_test.go`:

``` go
package proxy

import "testing"

func TestToSnobCase(t *testing.T) {
    tests := []struct {
        input, want string
    }{
        {"execute-command", "execute_command"},
        {"status", "status"},
        {"resource-read", "resource_read"},
        {"a-b-c", "a_b_c"},
        {"already_snake", "already_snake"},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            if got := toSnobCase(tt.input); got != tt.want {
                t.Errorf("toSnobCase(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}

func TestFromSnobCase(t *testing.T) {
    tests := []struct {
        input, want string
    }{
        {"execute_command", "execute-command"},
        {"status", "status"},
        {"resource_read", "resource-read"},
        {"a_b_c", "a-b-c"},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            if got := fromSnobCase(tt.input); got != tt.want {
                t.Errorf("fromSnobCase(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/proxy/... -v -run TestToSnobCase` Expected: FAIL ---
`toSnobCase` undefined

**Step 3: Write minimal implementation**

Add two functions to `internal/proxy/proxy.go` in the helpers section (after
`splitPrefix`):

``` go
func toSnobCase(name string) string {
    return strings.ReplaceAll(name, "-", "_")
}

func fromSnobCase(name string) string {
    return strings.ReplaceAll(name, "_", "-")
}
```

Modify `ListToolsV1` (line 127) --- change:

``` go
tool.Name = child.Client.Name() + "-" + tool.Name
```

to:

``` go
tool.Name = child.Client.Name() + "-" + toSnobCase(tool.Name)
```

Modify `CallToolV1` (lines 185-188) --- change:

``` go
params := protocol.ToolCallParams{
    Name:      toolName,
    Arguments: args,
}
```

to:

``` go
params := protocol.ToolCallParams{
    Name:      fromSnobCase(toolName),
    Arguments: args,
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/proxy/... -v` Expected: PASS

**Step 5: Commit**

    feat(proxy): convert tool names to snob-case

--------------------------------------------------------------------------------

### Task 3: Add snob-case conversion for prompts

**Promotion criteria:** N/A

**Files:** - Modify: `internal/proxy/proxy.go:471-472` (ListPromptsV1 prompt
name prefixing) - Modify: `internal/proxy/proxy.go:498-500` (GetPromptV1 prompt
name dispatch)

**Step 1: Modify ListPromptsV1**

Change line 472:

``` go
pr.Name = child.Client.Name() + "-" + pr.Name
```

to:

``` go
pr.Name = child.Client.Name() + "-" + toSnobCase(pr.Name)
```

**Step 2: Modify GetPromptV1**

Change the params construction (line 498-500):

``` go
params := protocol.PromptGetParams{
    Name:      promptName,
    Arguments: args,
}
```

to:

``` go
params := protocol.PromptGetParams{
    Name:      fromSnobCase(promptName),
    Arguments: args,
}
```

**Step 3: Run all tests**

Run: `go test ./... -v` Expected: PASS

**Step 4: Run bats tests**

Run: `just test-bats` Expected: The prompt bats tests will now need updating ---
prompt names will be snob-cased. Update `zz-tests_bats/prompt_proxying.bats`:

Change all occurrences of `test-greet` to `test-greet` (this prompt name has no
internal hyphens, so it stays the same). If the prompt server fixture uses
hyphenated prompt names, those tests would fail. Check the fixture --- the
prompt name is `greet` (no hyphens), so tests should still pass.

Expected: PASS

**Step 5: Commit**

    feat(proxy): convert prompt names to snob-case

--------------------------------------------------------------------------------

### Task 4: Inject synthetic resource tools into ListToolsV1

**Promotion criteria:** N/A

**Files:** - Modify: `internal/proxy/proxy.go:87-147` (ListToolsV1)

**Step 1: Write the bats test fixture**

The existing `resource-server.bash` already advertises resource capabilities and
handles `resources/list`, `resources/read`, and returns resource templates. But
it doesn't handle `resources/templates/list`. Update
`zz-tests_bats/test-fixtures/resource-server.bash` to add that handler:

Add a new case in the `case "$method"` block:

``` bash
resources/templates/list)
  echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"resourceTemplates":[{"uriTemplate":"test://items/{id}","name":"item","description":"Get item by ID","mimeType":"application/json"}]}}'
  ;;
```

**Step 2: Write the bats integration test**

Create `zz-tests_bats/synthetic_resource_tools.bats`:

``` bash
#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$(dirname "$BATS_TEST_FILE")/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function synthetic_resource_read_tool_appears_in_tools_list { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "res-resource_read")'
}

function synthetic_resource_templates_tool_appears_in_tools_list { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "res-resource_templates")'
}

function synthetic_resource_read_tool_reads_resource { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"res-resource_read","arguments":{"uri":"test://items"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text' | jq -e '. == "[1,2,3,4,5,6,7,8,9,10]"'
}

function synthetic_resource_templates_tool_returns_templates { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"res-resource_templates","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.content[0].text' | jq -e '.[0].uriTemplate == "test://items/{id}"'
}

function synthetic_tools_disabled_by_config { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
resource_tools = false
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  local count
  count=$(echo "$output" | jq '[.tools[] | select(.name == "res-resource_read" or .name == "res-resource_templates")] | length')
  [[ "$count" -eq 0 ]]
}

function synthetic_tools_not_generated_for_non_resource_servers { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = ["bash", "$FIXTURES_DIR/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  local count
  count=$(echo "$output" | jq '[.tools[] | select(.name == "test-resource_read" or .name == "test-resource_templates")] | length')
  [[ "$count" -eq 0 ]]
}
```

**Step 3: Run bats test to verify it fails**

Run: `just test-bats-file synthetic_resource_tools.bats` Expected: FAIL ---
synthetic tools not yet generated

**Step 4: Implement synthetic tool injection in ListToolsV1**

In `internal/proxy/proxy.go`, modify `ListToolsV1`. After the tool-collection
loop (after line 129), before the failed-server status tools (line 132), add:

``` go
// Inject synthetic resource tools for resource-capable children
for _, child := range p.children {
    if child.Capabilities.Resources == nil {
        continue
    }
    if child.Config.ResourceTools != nil && !*child.Config.ResourceTools {
        continue
    }

    serverName := child.Client.Name()

    // Check for collisions with child's own tools
    hasResourceRead := false
    hasResourceTemplates := false
    for _, t := range allTools {
        if t.Name == serverName+"-resource_read" {
            hasResourceRead = true
        }
        if t.Name == serverName+"-resource_templates" {
            hasResourceTemplates = true
        }
    }

    if !hasResourceRead {
        allTools = append(allTools, protocol.ToolV1{
            Name:        serverName + "-resource_read",
            Description: fmt.Sprintf("Read a resource from %s by URI", serverName),
            InputSchema: json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI"}},"required":["uri"]}`),
        })
    }

    if !hasResourceTemplates {
        allTools = append(allTools, protocol.ToolV1{
            Name:        serverName + "-resource_templates",
            Description: fmt.Sprintf("List available resource templates for %s", serverName),
            InputSchema: json.RawMessage(`{"type":"object"}`),
        })
    }
}
```

**Step 5: Run bats test again --- ListToolsV1 tests should pass but call tests
still fail**

Run: `just test-bats-file synthetic_resource_tools.bats` Expected: List tests
PASS, call tests FAIL

**Step 6: Commit**

    feat(proxy): inject synthetic resource tools into tools/list

--------------------------------------------------------------------------------

### Task 5: Handle synthetic tool calls in CallToolV1

**Promotion criteria:** N/A

**Files:** - Modify: `internal/proxy/proxy.go:149-210` (CallToolV1)

**Step 1: Add synthetic tool dispatch**

In `CallToolV1`, after the status-tool check (after line 176) and before the
`findChild` call (line 178), add handling for `resource_read` and
`resource_templates`. Actually, it's cleaner to add it after `findChild`
succeeds. Modify `CallToolV1` --- after line 183 (`findChild` error check),
before the `ToolCallParams` construction (line 185), add:

``` go
if toolName == "resource_read" {
    return p.callResourceRead(ctx, child, args)
}

if toolName == "resource_templates" {
    return p.callResourceTemplates(ctx, child)
}
```

Then add the two helper methods to `internal/proxy/proxy.go`:

``` go
func (p *Proxy) callResourceRead(
    ctx context.Context,
    child ChildEntry,
    args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
    var params struct {
        URI string `json:"uri"`
    }
    if err := json.Unmarshal(args, &params); err != nil {
        return protocol.ErrorResultV1(
            fmt.Sprintf("invalid resource_read args: %v", err),
        ), nil
    }

    raw, err := child.Client.Call(
        ctx,
        protocol.MethodResourcesRead,
        protocol.ResourceReadParams{URI: params.URI},
    )
    if err != nil {
        return nil, fmt.Errorf(
            "reading resource %s from %s: %w",
            params.URI,
            child.Client.Name(),
            err,
        )
    }

    var result protocol.ResourceReadResult
    if err := json.Unmarshal(raw, &result); err != nil {
        return nil, fmt.Errorf(
            "decoding resource read result from %s: %w",
            child.Client.Name(),
            err,
        )
    }

    text, err := json.Marshal(result.Contents)
    if err != nil {
        return nil, fmt.Errorf("marshaling resource contents: %w", err)
    }

    return &protocol.ToolCallResultV1{
        Content: []protocol.ContentBlockV1{
            {Type: "text", Text: string(text)},
        },
    }, nil
}

func (p *Proxy) callResourceTemplates(
    ctx context.Context,
    child ChildEntry,
) (*protocol.ToolCallResultV1, error) {
    raw, err := child.Client.Call(
        ctx,
        protocol.MethodResourcesTemplates,
        nil,
    )
    if err != nil {
        return nil, fmt.Errorf(
            "listing resource templates from %s: %w",
            child.Client.Name(),
            err,
        )
    }

    templates, err := decodeResourceTemplatesList(raw)
    if err != nil {
        return nil, fmt.Errorf(
            "decoding resource templates from %s: %w",
            child.Client.Name(),
            err,
        )
    }

    text, err := json.Marshal(templates)
    if err != nil {
        return nil, fmt.Errorf("marshaling resource templates: %w", err)
    }

    return &protocol.ToolCallResultV1{
        Content: []protocol.ContentBlockV1{
            {Type: "text", Text: string(text)},
        },
    }, nil
}
```

**Step 2: Run all tests**

Run: `just test` Expected: PASS (all Go + bats)

**Step 3: Commit**

    feat(proxy): handle resource_read and resource_templates tool calls

--------------------------------------------------------------------------------

### Task 6: Add snob-case bats test

**Promotion criteria:** N/A

**Files:** - Create: `zz-tests_bats/test-fixtures/tool-server.bash` - Create:
`zz-tests_bats/snob_case.bats`

**Step 1: Create a test fixture with hyphenated tool names**

Create `zz-tests_bats/test-fixtures/tool-server.bash`:

``` bash
#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server with hyphenated tool names.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"tool-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"execute-command","description":"Run a command","inputSchema":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}]}}'
    ;;
  tools/call)
    name=$(echo "$line" | jq -r '.params.name')
    case "$name" in
    execute-command)
      cmd=$(echo "$line" | jq -r '.params.arguments.cmd')
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"executed: '"$cmd"'"}]}}'
      ;;
    *)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"unknown tool: '"$name"'"}],"isError":true}}'
      ;;
    esac
    ;;
  esac
done
```

Make it executable: `chmod +x zz-tests_bats/test-fixtures/tool-server.bash`

**Step 2: Write the bats test**

Create `zz-tests_bats/snob_case.bats`:

``` bash
#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$(dirname "$BATS_TEST_FILE")/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function tool_names_use_snob_case { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # Hyphenated tool name should appear with underscores
  echo "$output" | jq -e '.tools[] | select(.name == "srv-execute_command")'
  # Original hyphenated form should NOT appear
  local hyphen_count
  hyphen_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv-execute-command")] | length')
  [[ "$hyphen_count" -eq 0 ]]
}

function snob_case_tool_call_dispatches_correctly { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"srv-execute_command","arguments":{"cmd":"hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: hello"'
}
```

**Step 3: Run tests**

Run: `just test-bats-file snob_case.bats` Expected: PASS

**Step 4: Commit**

    test: add snob-case and synthetic resource tools integration tests

--------------------------------------------------------------------------------

### Task 7: Run full test suite and verify

**Files:** None (validation only)

**Step 1: Run full test suite**

Run: `just` Expected: All build steps and tests pass

**Step 2: Run test-mcp recipe**

Run: `just test-mcp` Expected: `tools/list` output shows snob-cased tool names
and synthetic resource tools (if a moxyfile with resource-capable servers
exists)

**Step 3: Commit the design doc and plan**

    docs: add synthetic resource tools design and implementation plan
