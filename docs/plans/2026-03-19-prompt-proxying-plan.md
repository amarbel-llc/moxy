# Prompt Proxying Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Proxy `prompts/list` and `prompts/get` from child MCP servers, prefixing prompt names with `serverName-`.

**Architecture:** Mirror the tool proxy pattern exactly — iterate children with prompt capabilities, call via JSON-RPC, decode with V1→V0 fallback, prefix names with `serverName-`, split on first `-` for dispatch.

**Tech Stack:** Go, go-mcp (`protocol`, `server` packages), bats for integration tests.

**Rollback:** N/A — purely additive, no existing behavior changes.

---

### Task 1: Add prompt decode helpers

**Promotion criteria:** N/A

**Files:**
- Modify: `internal/proxy/proxy.go` (append after `decodeResourceTemplatesList`)

**Step 1: Write `decodePromptsList`**

Add after `decodeResourceTemplatesList` (line ~430):

```go
// decodePromptsList tries V1 first, falls back to V0 and upgrades.
func decodePromptsList(raw json.RawMessage) ([]protocol.PromptV1, error) {
	var v1 protocol.PromptsListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.Prompts) > 0 {
		return v1.Prompts, nil
	}

	var v0 protocol.PromptsListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		prompts := make([]protocol.PromptV1, len(v0.Prompts))
		for i, p := range v0.Prompts {
			prompts[i] = protocol.PromptV1{
				Name:        p.Name,
				Description: p.Description,
				Arguments:   p.Arguments,
			}
		}
		return prompts, nil
	}

	return nil, fmt.Errorf("unable to decode prompts list response")
}
```

**Step 2: Write `decodePromptGetResult`**

Add immediately after `decodePromptsList`:

```go
// decodePromptGetResult tries V1 first, falls back to V0 and upgrades.
func decodePromptGetResult(raw json.RawMessage) (*protocol.PromptGetResultV1, error) {
	var v1 protocol.PromptGetResultV1
	if err := json.Unmarshal(raw, &v1); err == nil {
		return &v1, nil
	}

	var v0 protocol.PromptGetResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		messages := make([]protocol.PromptMessageV1, len(v0.Messages))
		for i, m := range v0.Messages {
			messages[i] = protocol.PromptMessageV1{
				Role: m.Role,
				Content: protocol.ContentBlockV1{
					Type:     m.Content.Type,
					Text:     m.Content.Text,
					MimeType: m.Content.MimeType,
					Data:     m.Content.Data,
				},
			}
		}
		return &protocol.PromptGetResultV1{
			Description: v0.Description,
			Messages:    messages,
		}, nil
	}

	return nil, fmt.Errorf("unable to decode prompt get result")
}
```

**Step 3: Run tests to verify no regressions**

Run: `go vet ./... && go test ./...`
Expected: PASS (no new tests yet, just checking compilation)

**Step 4: Commit**

```
feat: add prompt decode helpers with V1/V0 fallback
```

---

### Task 2: Add PromptProviderV1 methods to Proxy

**Promotion criteria:** N/A

**Files:**
- Modify: `internal/proxy/proxy.go` (add between ResourceProviderV1 section and helpers section)

**Step 1: Add `ListPromptsV1`**

Insert before the `// --- helpers ---` comment (line ~286):

```go
// --- PromptProvider (V0) ---

func (p *Proxy) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	v1, err := p.ListPromptsV1(ctx, "")
	if err != nil {
		return nil, err
	}
	prompts := make([]protocol.Prompt, len(v1.Prompts))
	for i, pr := range v1.Prompts {
		prompts[i] = protocol.Prompt{
			Name:        pr.Name,
			Description: pr.Description,
			Arguments:   pr.Arguments,
		}
	}
	return prompts, nil
}

func (p *Proxy) GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.PromptGetResult, error) {
	v1, err := p.GetPromptV1(ctx, name, args)
	if err != nil {
		return nil, err
	}
	messages := make([]protocol.PromptMessage, len(v1.Messages))
	for i, m := range v1.Messages {
		messages[i] = protocol.PromptMessage{
			Role: m.Role,
			Content: protocol.ContentBlock{
				Type:     m.Content.Type,
				Text:     m.Content.Text,
				MimeType: m.Content.MimeType,
				Data:     m.Content.Data,
			},
		}
	}
	return &protocol.PromptGetResult{
		Description: v1.Description,
		Messages:    messages,
	}, nil
}

// --- PromptProviderV1 ---

func (p *Proxy) ListPromptsV1(ctx context.Context, cursor string) (*protocol.PromptsListResultV1, error) {
	allPrompts := make([]protocol.PromptV1, 0)

	for _, child := range p.children {
		if child.Capabilities.Prompts == nil {
			continue
		}

		raw, err := child.Client.Call(ctx, protocol.MethodPromptsList, cursorParams(cursor))
		if err != nil {
			p.markFailed(child.Client.Name(), fmt.Errorf("listing prompts: %w", err))
			continue
		}

		prompts, err := decodePromptsList(raw)
		if err != nil {
			p.markFailed(child.Client.Name(), fmt.Errorf("decoding prompts: %w", err))
			continue
		}

		for _, pr := range prompts {
			pr.Name = child.Client.Name() + "-" + pr.Name
			allPrompts = append(allPrompts, pr)
		}
	}

	return &protocol.PromptsListResultV1{Prompts: allPrompts}, nil
}

func (p *Proxy) GetPromptV1(ctx context.Context, name string, args map[string]string) (*protocol.PromptGetResultV1, error) {
	serverName, promptName, ok := splitPrefix(name, "-")
	if !ok {
		return nil, fmt.Errorf("invalid prompt name %q: missing server prefix", name)
	}

	child, ok := p.findChild(serverName)
	if !ok {
		return nil, fmt.Errorf("unknown server %q", serverName)
	}

	params := protocol.PromptGetParams{
		Name:      promptName,
		Arguments: args,
	}

	raw, err := child.Client.Call(ctx, protocol.MethodPromptsGet, params)
	if err != nil {
		return nil, fmt.Errorf("getting prompt %s from %s: %w", promptName, serverName, err)
	}

	result, err := decodePromptGetResult(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding prompt get result from %s: %w", serverName, err)
	}

	return result, nil
}
```

**Step 2: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

**Step 3: Commit**

```
feat: add PromptProviderV1 methods to Proxy
```

---

### Task 3: Register prompt provider in main.go

**Promotion criteria:** N/A

**Files:**
- Modify: `cmd/moxy/main.go:135-141` (add `Prompts: p` to server options)
- Modify: `cmd/moxy/main.go:158-159` (add type assertion)

**Step 1: Add `Prompts: p` to server.Options**

Change the `server.New` call (line ~135):

```go
	srv, err := server.New(t, server.Options{
		ServerName:    "moxy",
		ServerVersion: "0.1.0",
		Instructions:  "MCP proxy aggregating tools and resources from child servers.",
		Tools:         p,
		Resources:     p,
		Prompts:       p,
	})
```

**Step 2: Add type assertion**

After the existing assertions (line ~159):

```go
var _ server.PromptProviderV1 = (*proxy.Proxy)(nil)
```

**Step 3: Update Instructions string**

Change `"MCP proxy aggregating tools and resources from child servers."` to
`"MCP proxy aggregating tools, resources, and prompts from child servers."`

**Step 4: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

**Step 5: Commit**

```
feat: register prompt provider and update server instructions
```

---

### Task 4: Add bats integration test for prompt proxying

**Promotion criteria:** N/A

**Files:**
- Create: `zz-tests_bats/test-fixtures/prompt-server.bash` (mock MCP server with prompts)
- Modify: `zz-tests_bats/common.bash` (extend `run_moxy_mcp` to accept params)
- Create: `zz-tests_bats/prompt_proxying.bats`

**Step 1: Create a mock MCP server that exposes prompts**

Create `zz-tests_bats/test-fixtures/prompt-server.bash`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server that advertises prompt capabilities.
# Responds to initialize, prompts/list, and prompts/get.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
    initialize)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{}},"serverInfo":{"name":"prompt-test","version":"0.1"}}}'
      ;;
    notifications/initialized)
      # no response needed
      ;;
    prompts/list)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"prompts":[{"name":"greet","description":"Generate a greeting","arguments":[{"name":"name","description":"Name to greet","required":true}]}]}}'
      ;;
    prompts/get)
      name=$(echo "$line" | jq -r '.params.name')
      arg_name=$(echo "$line" | jq -r '.params.arguments.name // "world"')
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"description":"A greeting","messages":[{"role":"user","content":{"type":"text","text":"Hello, '"$arg_name"'!"}}]}}'
      ;;
  esac
done
```

Make it executable: `chmod +x zz-tests_bats/test-fixtures/prompt-server.bash`

**Step 2: Extend `run_moxy_mcp` to accept optional params**

Modify `common.bash` to support an optional second argument for JSON params:

```bash
run_moxy_mcp() {
  local method="$1"
  shift
  local params="${1:-}"

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local method_req
  if [[ -n "$params" ]]; then
    method_req=$(jq -cn --arg m "$method" --argjson p "$params" '{"jsonrpc":"2.0","id":2,"method":$m,"params":$p}')
  else
    method_req=$(jq -cn --arg m "$method" '{"jsonrpc":"2.0","id":2,"method":$m}')
  fi

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>/dev/null | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$init" "$initialized" "$method_req"
}
```

**Step 3: Write the bats test file**

Create `zz-tests_bats/prompt_proxying.bats`:

```bash
#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function prompts_list_returns_prefixed_names { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = "bash"
args = ["$(dirname "$BATS_TEST_FILE")/test-fixtures/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts[] | select(.name == "test-greet")'
}

function prompts_list_preserves_description { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = "bash"
args = ["$(dirname "$BATS_TEST_FILE")/test-fixtures/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts[] | select(.name == "test-greet") | .description == "Generate a greeting"'
}

function prompts_list_preserves_arguments { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = "bash"
args = ["$(dirname "$BATS_TEST_FILE")/test-fixtures/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts[] | select(.name == "test-greet") | .arguments[0].name == "name"'
}

function prompts_get_dispatches_to_child { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = "bash"
args = ["$(dirname "$BATS_TEST_FILE")/test-fixtures/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/get '{"name":"test-greet","arguments":{"name":"Alice"}}'
  assert_success
  echo "$output" | jq -e '.messages[0].content.text == "Hello, Alice!"'
}

function prompts_list_skips_servers_without_capability { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts | length == 0'
}
```

**Step 4: Run the bats tests**

Run: `just test-bats`
Expected: All prompt_proxying tests PASS

**Step 5: Commit**

```
test: add bats integration tests for prompt proxying
```
