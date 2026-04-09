# Config-as-Server Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Enable declaring MCP tools as TOML configs in `.moxy/` directories, with moxy handling process invocation, result caching, and resource-as-fd composition.

**Architecture:** Extract a `ServerBackend` interface from the proxy so both proxied MCP servers (`mcpclient.Client`) and config-declared virtual servers (`native.Server`) satisfy the same contract. Config files in `.moxy/` directories are discovered via the same hierarchy walk as moxyfiles. Tool calls spawn the declared binary per-invocation, cache results automatically, and support URI-to-fd rewriting for composability.

**Tech Stack:** Go, TOML (tommy codegen), existing go-mcp protocol types.

**Rollback:** Remove `.moxy/` configs to revert. The `ServerBackend` interface extraction is additive — `mcpclient.Client` already satisfies it. No existing binaries are modified.

---

### Task 1: Extract ServerBackend Interface

**Promotion criteria:** N/A (additive refactor, no old approach to remove)

**Files:**
- Create: `internal/proxy/backend.go`
- Modify: `internal/proxy/proxy.go:19-25` (ChildEntry struct)
- Modify: `internal/proxy/proxy.go:43-46` (ConnectFunc type)
- Modify: `cmd/moxy/main.go:184-190` (connectServer func)
- Test: existing tests in `internal/proxy/*_test.go` must continue passing

**Step 1: Write the interface file**

Create `internal/proxy/backend.go`:

```go
package proxy

import (
	"context"
	"encoding/json"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
)

// ServerBackend abstracts the proxy's interaction with child servers.
// Both proxied MCP servers (mcpclient.Client) and config-declared virtual
// servers (native.Server) implement this interface.
type ServerBackend interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	Notify(method string, params any) error
	SetOnNotification(fn func(*jsonrpc.Message))
	Name() string
	Close() error
}
```

**Step 2: Update ChildEntry to use the interface**

In `internal/proxy/proxy.go`, change `ChildEntry.Client` from `*mcpclient.Client` to `ServerBackend`:

```go
type ChildEntry struct {
	Client       ServerBackend
	Config       config.ServerConfig
	Capabilities protocol.ServerCapabilitiesV1
	ServerInfo   protocol.ImplementationV1
	Instructions string
}
```

**Step 3: Update ConnectFunc signature**

In `internal/proxy/proxy.go`, change the return type:

```go
type ConnectFunc func(ctx context.Context, cfg config.ServerConfig) (ServerBackend, *protocol.InitializeResultV1, error)
```

**Step 4: Update main.go connectServer**

In `cmd/moxy/main.go`, the `connectServer` closure's return type must match the new `ConnectFunc` signature. Since `*mcpclient.Client` satisfies `ServerBackend`, this only requires changing the function signature, not the body:

```go
connectServer := func(ctx context.Context, srvCfg config.ServerConfig) (proxy.ServerBackend, *protocol.InitializeResultV1, error) {
```

Find every other place `ConnectFunc` is referenced or where `*mcpclient.Client` is returned to the proxy and update accordingly.

**Step 5: Run all Go tests**

Run: `go test ./...`
Expected: all existing tests pass (no test references `ChildEntry.Client` as `*mcpclient.Client`)

**Step 6: Run bats integration tests**

Run: `just test-bats`
Expected: all 18 bats test files pass

**Step 7: Commit**

```
feat(proxy): extract ServerBackend interface from ChildEntry

Enables config-declared virtual servers to satisfy the same contract
as proxied MCP servers. No behavioral change — mcpclient.Client
already satisfies the interface.
```

---

### Task 2: Native Server Config Parsing

**Promotion criteria:** N/A (new feature)

**Files:**
- Create: `internal/native/config.go`
- Create: `internal/native/config_test.go`

**Step 1: Write the failing test**

Create `internal/native/config_test.go`:

```go
package native

import (
	"testing"
)

func TestParseConfig(t *testing.T) {
	data := []byte(`
name = "shell"
description = "Shell execution"

[[tools]]
name = "exec"
description = "Execute a shell command"
command = "sh"
args = ["-c"]

[tools.input.properties.command]
type = "string"
description = "Shell command to execute"

[tools.input]
required = ["command"]
`)

	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Name != "shell" {
		t.Errorf("name = %q, want %q", cfg.Name, "shell")
	}
	if cfg.Description != "Shell execution" {
		t.Errorf("description = %q, want %q", cfg.Description, "Shell execution")
	}
	if len(cfg.Tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(cfg.Tools))
	}

	tool := cfg.Tools[0]
	if tool.Name != "exec" {
		t.Errorf("tool.name = %q, want %q", tool.Name, "exec")
	}
	if tool.Command != "sh" {
		t.Errorf("tool.command = %q, want %q", tool.Command, "sh")
	}
	if len(tool.Args) != 1 || tool.Args[0] != "-c" {
		t.Errorf("tool.args = %v, want ['-c']", tool.Args)
	}
}

func TestParseConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "missing server name",
			data: `description = "no name"`,
		},
		{
			name: "dots in server name",
			data: `name = "my.server"`,
		},
		{
			name: "tool missing name",
			data: `
name = "shell"
[[tools]]
command = "echo"
`,
		},
		{
			name: "tool missing command",
			data: `
name = "shell"
[[tools]]
name = "exec"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.data))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/native/... -v`
Expected: FAIL (package doesn't exist yet)

**Step 3: Write minimal implementation**

Create `internal/native/config.go`:

```go
package native

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

type NativeConfig struct {
	Name        string     `toml:"name"`
	Description string     `toml:"description"`
	Tools       []ToolSpec `toml:"tools"`
}

type ToolSpec struct {
	Name        string          `toml:"name"`
	Description string          `toml:"description"`
	Command     string          `toml:"command"`
	Args        []string        `toml:"args"`
	Input       json.RawMessage `toml:"input"`
}

func ParseConfig(data []byte) (*NativeConfig, error) {
	var cfg NativeConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing native config: %w", err)
	}

	if cfg.Name == "" {
		return nil, fmt.Errorf("native config: name is required")
	}
	if strings.Contains(cfg.Name, ".") {
		return nil, fmt.Errorf("native config: name %q must not contain '.'", cfg.Name)
	}
	for i, tool := range cfg.Tools {
		if tool.Name == "" {
			return nil, fmt.Errorf("native config %q: tool[%d] missing name", cfg.Name, i)
		}
		if tool.Command == "" {
			return nil, fmt.Errorf("native config %q: tool %q missing command", cfg.Name, tool.Name)
		}
	}

	return &cfg, nil
}
```

Note: The `Input` field uses `json.RawMessage` for now. TOML tables decoded into
`json.RawMessage` may need an intermediate map step — adjust during prototyping.
The exact type will be refined when we wire up the JSON schema to MCP tool
registration.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/native/... -v`
Expected: PASS

**Step 5: Commit**

```
feat(native): add config parsing for .moxy/ tool declarations
```

---

### Task 3: .moxy/ Directory Discovery

**Promotion criteria:** N/A (new feature)

**Files:**
- Create: `internal/native/discovery.go`
- Create: `internal/native/discovery_test.go`

**Step 1: Write the failing test**

Create `internal/native/discovery_test.go`:

```go
package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverConfigs(t *testing.T) {
	// Create a temp hierarchy:
	// home/.config/moxy/.moxy/global.toml
	// home/project/.moxy/local.toml
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalMoxy := filepath.Join(home, ".config", "moxy", ".moxy")
	os.MkdirAll(globalMoxy, 0o755)
	os.WriteFile(filepath.Join(globalMoxy, "global.toml"), []byte(`
name = "global-tool"
[[tools]]
name = "hello"
command = "echo"
args = ["hello"]
`), 0o644)

	localMoxy := filepath.Join(project, ".moxy")
	os.MkdirAll(localMoxy, 0o755)
	os.WriteFile(filepath.Join(localMoxy, "local.toml"), []byte(`
name = "local-tool"
[[tools]]
name = "world"
command = "echo"
args = ["world"]
`), 0o644)

	configs, err := DiscoverConfigs(home, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 2 {
		t.Fatalf("len(configs) = %d, want 2", len(configs))
	}

	names := map[string]bool{}
	for _, cfg := range configs {
		names[cfg.Name] = true
	}
	if !names["global-tool"] {
		t.Error("expected global-tool in discovered configs")
	}
	if !names["local-tool"] {
		t.Error("expected local-tool in discovered configs")
	}
}

func TestDiscoverConfigsOverride(t *testing.T) {
	// Later .moxy/ directory overrides earlier by server name
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalMoxy := filepath.Join(home, ".config", "moxy", ".moxy")
	os.MkdirAll(globalMoxy, 0o755)
	os.WriteFile(filepath.Join(globalMoxy, "shell.toml"), []byte(`
name = "shell"
description = "global"
[[tools]]
name = "exec"
command = "sh"
`), 0o644)

	localMoxy := filepath.Join(project, ".moxy")
	os.MkdirAll(localMoxy, 0o755)
	os.WriteFile(filepath.Join(localMoxy, "shell.toml"), []byte(`
name = "shell"
description = "local"
[[tools]]
name = "exec"
command = "bash"
`), 0o644)

	configs, err := DiscoverConfigs(home, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Description != "local" {
		t.Errorf("expected local override, got description=%q", configs[0].Description)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/native/... -v -run TestDiscover`
Expected: FAIL (function doesn't exist)

**Step 3: Write minimal implementation**

Create `internal/native/discovery.go`:

```go
package native

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverConfigs walks the .moxy/ directory hierarchy from home to dir,
// loading *.toml files and merging by server name (later overrides earlier).
func DiscoverConfigs(home, dir string) ([]*NativeConfig, error) {
	byName := make(map[string]*NativeConfig)
	var order []string

	loadDir := func(moxyDir string) error {
		entries, err := os.ReadDir(moxyDir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", moxyDir, err)
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(moxyDir, e.Name()))
			if err != nil {
				return fmt.Errorf("reading %s: %w", e.Name(), err)
			}
			cfg, err := ParseConfig(data)
			if err != nil {
				return fmt.Errorf("%s/%s: %w", moxyDir, e.Name(), err)
			}
			if _, exists := byName[cfg.Name]; !exists {
				order = append(order, cfg.Name)
			}
			byName[cfg.Name] = cfg
		}
		return nil
	}

	// 1. Global: ~/.config/moxy/.moxy/
	globalDir := filepath.Join(home, ".config", "moxy", ".moxy")
	if err := loadDir(globalDir); err != nil {
		return nil, err
	}

	// 2. Intermediate parent directories
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)
	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			if err := loadDir(filepath.Join(parentDir, ".moxy")); err != nil {
				return nil, err
			}
		}
	}

	// 3. Target directory
	if err := loadDir(filepath.Join(cleanDir, ".moxy")); err != nil {
		return nil, err
	}

	result := make([]*NativeConfig, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	return result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/native/... -v -run TestDiscover`
Expected: PASS

**Step 5: Commit**

```
feat(native): add .moxy/ directory discovery with hierarchy walk
```

---

### Task 4: Native Server Backend

**Promotion criteria:** N/A (new feature)

**Files:**
- Create: `internal/native/server.go`
- Create: `internal/native/server_test.go`

This is the core — `native.Server` implements `proxy.ServerBackend`. For the
MVP, it handles `tools/list` and `tools/call` methods. Other methods
(`resources/*`, `prompts/*`) return empty results.

**Step 1: Write the failing test**

Create `internal/native/server_test.go`:

```go
package native

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

func TestServerToolsList(t *testing.T) {
	cfg := &NativeConfig{
		Name:        "shell",
		Description: "Shell execution",
		Tools: []ToolSpec{
			{
				Name:        "exec",
				Description: "Execute a command",
				Command:     "echo",
				Args:        []string{"-n"},
			},
		},
	}

	srv := NewServer(cfg)
	raw, err := srv.Call(context.Background(), protocol.MethodToolsList, nil)
	if err != nil {
		t.Fatalf("Call tools/list: %v", err)
	}

	var result protocol.ListToolsResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(result.Tools))
	}
	if result.Tools[0].Name != "exec" {
		t.Errorf("tool name = %q, want %q", result.Tools[0].Name, "exec")
	}
}

func TestServerToolsCall(t *testing.T) {
	cfg := &NativeConfig{
		Name: "shell",
		Tools: []ToolSpec{
			{
				Name:    "exec",
				Command: "echo",
				Args:    []string{"-n", "hello world"},
			},
		},
	}

	srv := NewServer(cfg)
	params := protocol.CallToolParamsV1{
		Name: "exec",
	}
	raw, err := srv.Call(context.Background(), protocol.MethodToolsCall, params)
	if err != nil {
		t.Fatalf("Call tools/call: %v", err)
	}

	var result protocol.CallToolResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content, got empty")
	}
	if result.Content[0].Text != "hello world" {
		t.Errorf("output = %q, want %q", result.Content[0].Text, "hello world")
	}
}

func TestServerName(t *testing.T) {
	srv := NewServer(&NativeConfig{Name: "test-server"})
	if srv.Name() != "test-server" {
		t.Errorf("Name() = %q, want %q", srv.Name(), "test-server")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/native/... -v -run TestServer`
Expected: FAIL (NewServer doesn't exist)

**Step 3: Write minimal implementation**

Create `internal/native/server.go`:

```go
package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// Server implements proxy.ServerBackend for config-declared virtual servers.
type Server struct {
	config *NativeConfig
	tools  map[string]*ToolSpec
}

func NewServer(cfg *NativeConfig) *Server {
	tools := make(map[string]*ToolSpec, len(cfg.Tools))
	for i := range cfg.Tools {
		tools[cfg.Tools[i].Name] = &cfg.Tools[i]
	}
	return &Server{config: cfg, tools: tools}
}

func (s *Server) Name() string { return s.config.Name }

func (s *Server) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	switch method {
	case protocol.MethodToolsList:
		return s.handleToolsList()
	case protocol.MethodToolsCall:
		return s.handleToolsCall(ctx, params)
	case protocol.MethodResourcesList, protocol.MethodResourcesTemplates,
		protocol.MethodPromptsList:
		return json.Marshal(struct{}{})
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

func (s *Server) Notify(method string, params any) error { return nil }

func (s *Server) SetOnNotification(fn func(*jsonrpc.Message)) {}

func (s *Server) Close() error { return nil }

func (s *Server) handleToolsList() (json.RawMessage, error) {
	var tools []protocol.ToolV1
	for _, spec := range s.config.Tools {
		tool := protocol.ToolV1{
			Name:        spec.Name,
			Description: spec.Description,
		}
		if spec.Input != nil {
			tool.InputSchema = spec.Input
		}
		tools = append(tools, tool)
	}
	return json.Marshal(protocol.ListToolsResultV1{Tools: tools})
}

func (s *Server) handleToolsCall(ctx context.Context, params any) (json.RawMessage, error) {
	// Marshal and re-parse params to handle both struct and map inputs.
	data, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling params: %w", err)
	}
	var callParams protocol.CallToolParamsV1
	if err := json.Unmarshal(data, &callParams); err != nil {
		return nil, fmt.Errorf("parsing call params: %w", err)
	}

	spec, ok := s.tools[callParams.Name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", callParams.Name)
	}

	return s.execTool(ctx, spec, callParams.Arguments)
}

func (s *Server) execTool(
	ctx context.Context,
	spec *ToolSpec,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	// For now: run command+args directly. The mapping from tool arguments
	// to process input (additional args, stdin, env) will be refined
	// during prototyping.
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()

	isError := err != nil
	if isError && output == "" {
		output = stderr.String()
	}

	result := protocol.CallToolResultV1{
		Content: []protocol.ContentV1{
			{Type: "text", Text: output},
		},
		IsError: isError,
	}

	return json.Marshal(result)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/native/... -v -run TestServer`
Expected: PASS

**Step 5: Commit**

```
feat(native): implement ServerBackend for config-declared tools
```

---

### Task 5: Wire Native Servers into Proxy Startup

**Promotion criteria:** N/A (new feature)

**Files:**
- Modify: `cmd/moxy/main.go` (add native server discovery and registration)
- Test: `zz-tests_bats/` (new bats test)
- Create: `zz-tests_bats/native_server.bats`

**Step 1: Write the failing bats test**

Create `zz-tests_bats/native_server.bats`:

```bash
#!/usr/bin/env bats

load common

setup() {
  export HOME="$(mktemp -d)"
  mkdir -p "$HOME/project/.moxy"
  cat > "$HOME/project/.moxy/greeter.toml" << 'EOF'
name = "greeter"

[[tools]]
name = "hello"
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF
  cd "$HOME/project"
}

@test "native server tool appears in tools/list" {
  run_moxy_mcp "tools/list"
  assert_success
  assert_output --partial '"name":"greeter.hello"'
}

@test "native server tool can be called" {
  local params='{"name":"greeter.hello"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "hello world"
}
```

**Step 2: Run test to verify it fails**

Run: `just test-bats-file native_server.bats`
Expected: FAIL (moxy doesn't know about .moxy/ configs yet)

**Step 3: Wire native servers into main.go**

In `cmd/moxy/main.go`, after loading the moxyfile hierarchy, add native server
discovery and registration. The exact integration point is after the
`connectServer` loop that creates `children`:

1. Call `native.DiscoverConfigs(home, cwd)` to find `.moxy/*.toml` files
2. For each config, create a `native.Server`
3. Synthesize `InitializeResultV1` with `Capabilities.Tools` set
4. Append to `children` as `proxy.ChildEntry` entries

The native servers need a `InitializeResult()` method on `native.Server` that
returns synthetic capabilities — add this to `server.go`.

**Step 4: Run the bats test**

Run: `just test-bats-file native_server.bats`
Expected: PASS

**Step 5: Run all tests to verify no regressions**

Run: `just test`
Expected: all tests pass

**Step 6: Commit**

```
feat: wire native .moxy/ servers into proxy startup
```

---

### Task 6: Result Caching

**Promotion criteria:** N/A (new feature)

**Files:**
- Create: `internal/native/cache.go`
- Create: `internal/native/cache_test.go`
- Modify: `internal/native/server.go` (add caching to execTool)

Port the caching mechanism from `cmd/maneater/exec_cache.go` into the native
package. The same logic: cache dir at `$XDG_CACHE_HOME/moxy/native-results/`,
token threshold of 50, head/tail summary, URI format
`moxy.native://results/{session}/{id}`.

**Step 1: Write the failing test**

```go
func TestResultCaching(t *testing.T) {
	cache := newResultCache(t.TempDir())
	result := cachedResult{
		ID:      "test-id",
		Session: "test-session",
		Command: "echo hello",
		Output:  "hello\n",
	}

	if err := cache.store(result); err != nil {
		t.Fatalf("store: %v", err)
	}

	loaded, err := cache.load("test-session", "test-id")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Output != "hello\n" {
		t.Errorf("output = %q, want %q", loaded.Output, "hello\n")
	}
}

func TestSummaryFormat(t *testing.T) {
	// Generate output exceeding threshold
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	output := strings.Join(lines, "\n") + "\n"

	result := cachedResult{
		ID:         "sum-id",
		Session:    "sum-session",
		Command:    "seq 30",
		Output:     output,
		LineCount:  30,
		TokenCount: estimateTokens(output),
	}

	summary := formatSummary(result)
	if !strings.Contains(summary, "First 10 lines") {
		t.Error("expected head section in summary")
	}
	if !strings.Contains(summary, "Last 10 lines") {
		t.Error("expected tail section in summary")
	}
	if !strings.Contains(summary, "moxy.native://results/sum-session/sum-id") {
		t.Error("expected result URI in summary")
	}
}
```

**Step 2-4: Implement, test, verify**

Port `exec_cache.go` logic with the URI prefix changed from
`maneater.exec://results/` to `moxy.native://results/`. Update `execTool` in
`server.go` to cache results when token count exceeds threshold, returning the
summary instead of full output.

**Step 5: Commit**

```
feat(native): add result caching for tool outputs
```

---

### Task 7: Resource-as-fd Substitution

**Promotion criteria:** N/A (new feature)

**Files:**
- Create: `internal/native/substitute.go`
- Create: `internal/native/substitute_test.go`
- Modify: `internal/native/server.go` (apply substitution before exec)

Port `cmd/maneater/exec_substitute.go` into the native package, matching the
`moxy.native://results/` URI pattern. Apply substitution in `execTool` before
spawning the process.

**Step 1: Write the failing test**

```go
func TestSubstituteURIs(t *testing.T) {
	cache := newResultCache(t.TempDir())
	cache.store(cachedResult{
		ID:      "abc",
		Session: "sess",
		Output:  "cached content",
	})

	sub, err := substituteResultURIs(
		"grep pattern moxy.native://results/sess/abc",
		cache,
	)
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	defer sub.Cleanup()

	if strings.Contains(sub.Command, "moxy.native://") {
		t.Error("URI was not rewritten")
	}
	if !strings.Contains(sub.Command, "/dev/fd/") {
		t.Error("expected /dev/fd/N in rewritten command")
	}
	if len(sub.ExtraFiles) != 1 {
		t.Errorf("len(ExtraFiles) = %d, want 1", len(sub.ExtraFiles))
	}
}
```

**Step 2-4: Implement, test, verify**

Port `exec_substitute.go` with the URI pattern changed to match
`moxy.native://results/`. Wire into `execTool`: before spawning the command,
call `substituteResultURIs` on the command string, set `cmd.ExtraFiles`, call
`StartWriters` after `cmd.Start`.

**Step 5: Add bats integration test**

Create a bats test that:
1. Calls a native tool that produces output exceeding the token threshold
2. Extracts the cached result URI from the summary
3. Calls a second tool with the URI in the command string
4. Verifies the cached content was available to the second command

**Step 6: Commit**

```
feat(native): add resource-as-fd substitution for cached results
```

---

### Task 8: End-to-end Prototype with shell.toml

**Promotion criteria:** config-based exec produces identical results to maneater exec for the same commands over 1 week of daily use.

**Files:**
- Create: sample `.moxy/shell.toml` config (in test fixtures or docs)
- Create: `zz-tests_bats/native_exec_e2e.bats`

Write a bats test that mimics the maneater exec workflow:

1. Set up a `.moxy/shell.toml` that declares an `exec` tool running `sh -c`
2. Call the tool with a command that produces large output (exceeds token threshold)
3. Verify the result is a cached summary with a URI
4. Call the tool again with the URI embedded in the command string
5. Verify the cached content was streamed via `/dev/fd/N`

This validates the full pipeline: config discovery → tool registration →
process invocation → result caching → resource-as-fd composition.

**Step 1: Write the test**

**Step 2: Run and verify**

Run: `just test-bats-file native_exec_e2e.bats`
Expected: PASS

**Step 3: Run full test suite**

Run: `just test`
Expected: all tests pass

**Step 4: Commit**

```
test: add end-to-end bats test for config-as-server exec pipeline
```
