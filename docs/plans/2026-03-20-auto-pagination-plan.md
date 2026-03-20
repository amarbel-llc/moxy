# Auto-Pagination Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Add opt-in pagination for JSON array resource responses, controlled by
`paginate = true` in moxyfile server entries.

**Architecture:** New `Paginate` field on `ServerConfig`. The proxy's
`ReadResource` path parses `?offset=N&limit=M` from the URI, forwards the clean
URI to the child, then slices the JSON array response and wraps it with
`{"items": [...], "total": N, "offset": N, "limit": N}`. A new
`internal/paginate` package handles URI parsing and array slicing.

**Tech Stack:** Go, `encoding/json`, `net/url`

**Rollback:** Remove `paginate = true` from moxyfile. No other changes needed.

--------------------------------------------------------------------------------

### Task 1: Add `Paginate` field to config

**Files:** - Modify: `internal/config/config.go:19-23` - Test:
`internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

``` go
func TestParsePaginate(t *testing.T) {
    input := `
[[servers]]
name = "caldav"
command = "caldav-mcp"
paginate = true
`
    cfg, err := Parse([]byte(input))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !cfg.Servers[0].Paginate {
        t.Error("expected paginate = true")
    }
}

func TestParsePaginateDefault(t *testing.T) {
    input := `
[[servers]]
name = "grit"
command = "grit"
`
    cfg, err := Parse([]byte(input))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cfg.Servers[0].Paginate {
        t.Error("expected paginate = false by default")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run TestParsePaginate` Expected: FAIL
--- `cfg.Servers[0].Paginate` undefined

**Step 3: Write minimal implementation**

In `internal/config/config.go`, add `Paginate` to `ServerConfig`:

``` go
type ServerConfig struct {
    Name        string            `toml:"name"`
    Command     Command           `toml:"command"`
    Annotations *AnnotationFilter `toml:"annotations"`
    Paginate    bool              `toml:"paginate"`
}
```

In the `Parse` function, after `parseAnnotations`, add:

``` go
paginate, _ := document.GetFromContainer[bool](doc, node, "paginate")
cfg.Servers[i].Paginate = paginate
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run TestParsePaginate` Expected: PASS

**Step 5: Commit**

    feat(config): add paginate field to ServerConfig

--------------------------------------------------------------------------------

### Task 2: Create `internal/paginate` package --- URI parsing

**Files:** - Create: `internal/paginate/paginate.go` - Create:
`internal/paginate/paginate_test.go`

**Step 1: Write the failing test**

Create `internal/paginate/paginate_test.go`:

``` go
package paginate

import "testing"

func TestParseParams(t *testing.T) {
    tests := []struct {
        name      string
        uri       string
        wantClean string
        wantOff   int
        wantLim   int
        wantOk    bool
    }{
        {"no params", "caldav://tasks", "caldav://tasks", 0, 0, false},
        {"offset only", "caldav://tasks?offset=10", "caldav://tasks", 10, 50, true},
        {"both", "caldav://tasks?offset=0&limit=25", "caldav://tasks", 0, 25, true},
        {"limit only ignored", "caldav://tasks?limit=25", "caldav://tasks", 0, 0, false},
        {"preserves other params", "caldav://tasks?foo=bar&offset=5&limit=10", "caldav://tasks?foo=bar", 5, 10, true},
        {"offset zero", "caldav://tasks?offset=0", "caldav://tasks", 0, 50, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            clean, params := ParseParams(tt.uri)
            if clean != tt.wantClean {
                t.Errorf("clean URI: got %q, want %q", clean, tt.wantClean)
            }
            if params.Active != tt.wantOk {
                t.Errorf("active: got %v, want %v", params.Active, tt.wantOk)
            }
            if params.Active {
                if params.Offset != tt.wantOff {
                    t.Errorf("offset: got %d, want %d", params.Offset, tt.wantOff)
                }
                if params.Limit != tt.wantLim {
                    t.Errorf("limit: got %d, want %d", params.Limit, tt.wantLim)
                }
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/paginate/... -v -run TestParseParams` Expected: FAIL
--- package doesn't exist

**Step 3: Write minimal implementation**

Create `internal/paginate/paginate.go`:

``` go
package paginate

import (
    "net/url"
    "strconv"
    "strings"
)

const DefaultLimit = 50

type Params struct {
    Active bool
    Offset int
    Limit  int
}

// ParseParams extracts offset/limit from URI query params.
// Returns the cleaned URI (without pagination params) and the parsed params.
// Pagination is only active when offset is present.
func ParseParams(uri string) (string, Params) {
    // Split on first '?'
    base, query, hasQuery := strings.Cut(uri, "?")
    if !hasQuery {
        return uri, Params{}
    }

    values, err := url.ParseQuery(query)
    if err != nil {
        return uri, Params{}
    }

    offsetStr := values.Get("offset")
    if offsetStr == "" {
        return uri, Params{}
    }

    offset, err := strconv.Atoi(offsetStr)
    if err != nil {
        return uri, Params{}
    }

    limit := DefaultLimit
    if limitStr := values.Get("limit"); limitStr != "" {
        if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
            limit = l
        }
    }

    // Rebuild URI without pagination params
    values.Del("offset")
    values.Del("limit")
    clean := base
    if remaining := values.Encode(); remaining != "" {
        clean = base + "?" + remaining
    }

    return clean, Params{Active: true, Offset: offset, Limit: limit}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/paginate/... -v -run TestParseParams` Expected: PASS

**Step 5: Commit**

    feat(paginate): add URI param parsing for offset/limit

--------------------------------------------------------------------------------

### Task 3: Add JSON array slicing and wrapping to `internal/paginate`

**Files:** - Modify: `internal/paginate/paginate.go` - Modify:
`internal/paginate/paginate_test.go`

**Step 1: Write the failing test**

Add to `internal/paginate/paginate_test.go`:

``` go
func TestSliceArray(t *testing.T) {
    input := `[1,2,3,4,5,6,7,8,9,10]`

    tests := []struct {
        name       string
        offset     int
        limit      int
        wantItems  string
        wantTotal  int
        wantOffset int
        wantLimit  int
    }{
        {"first page", 0, 3, "[1,2,3]", 10, 0, 3},
        {"middle page", 3, 3, "[4,5,6]", 10, 3, 3},
        {"last page partial", 8, 3, "[9,10]", 10, 8, 3},
        {"offset beyond end", 20, 3, "[]", 10, 20, 3},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := SliceArray(input, Params{Active: true, Offset: tt.offset, Limit: tt.limit})
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if result.Total != tt.wantTotal {
                t.Errorf("total: got %d, want %d", result.Total, tt.wantTotal)
            }
            if result.Offset != tt.wantOffset {
                t.Errorf("offset: got %d, want %d", result.Offset, tt.wantOffset)
            }
            if result.Limit != tt.wantLimit {
                t.Errorf("limit: got %d, want %d", result.Limit, tt.wantLimit)
            }
        })
    }
}

func TestSliceArrayNotArray(t *testing.T) {
    input := `{"key": "value"}`
    _, err := SliceArray(input, Params{Active: true, Offset: 0, Limit: 10})
    if err != ErrNotArray {
        t.Errorf("expected ErrNotArray, got %v", err)
    }
}

func TestSliceArrayInactiveParams(t *testing.T) {
    _, err := SliceArray(`[1,2,3]`, Params{Active: false})
    if err != ErrNotActive {
        t.Errorf("expected ErrNotActive, got %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/paginate/... -v -run TestSliceArray` Expected: FAIL ---
`SliceArray` undefined

**Step 3: Write minimal implementation**

Add to `internal/paginate/paginate.go`:

``` go
import (
    "encoding/json"
    "errors"
    // ... existing imports
)

var (
    ErrNotArray  = errors.New("content is not a JSON array")
    ErrNotActive = errors.New("pagination not active")
)

type Result struct {
    Items  json.RawMessage `json:"items"`
    Total  int             `json:"total"`
    Offset int             `json:"offset"`
    Limit  int             `json:"limit"`
}

// SliceArray parses text as a JSON array, slices it, and returns
// a Result with the page and metadata. Returns ErrNotArray if the
// content is not a JSON array, ErrNotActive if params are inactive.
func SliceArray(text string, params Params) (*Result, error) {
    if !params.Active {
        return nil, ErrNotActive
    }

    var items []json.RawMessage
    if err := json.Unmarshal([]byte(text), &items); err != nil {
        return nil, ErrNotArray
    }

    total := len(items)
    start := params.Offset
    if start > total {
        start = total
    }
    end := start + params.Limit
    if end > total {
        end = total
    }

    page := items[start:end]
    pageJSON, err := json.Marshal(page)
    if err != nil {
        return nil, err
    }

    return &Result{
        Items:  pageJSON,
        Total:  total,
        Offset: params.Offset,
        Limit:  params.Limit,
    }, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/paginate/... -v` Expected: PASS

**Step 5: Commit**

    feat(paginate): add JSON array slicing and response wrapping

--------------------------------------------------------------------------------

### Task 4: Wire pagination into proxy `ReadResource`

**Files:** - Modify: `internal/proxy/proxy.go:183-207`

**Step 1: Write the failing test**

This is best tested via bats integration tests (Task 5). For this task, verify
the wiring compiles and existing tests still pass.

**Step 2: Write the implementation**

Modify `ReadResource` in `internal/proxy/proxy.go`:

``` go
func (p *Proxy) ReadResource(ctx context.Context, uri string) (*protocol.ResourceReadResult, error) {
    serverName, originalURI, ok := splitPrefix(uri, "/")
    if !ok {
        return nil, fmt.Errorf("invalid resource URI %q: missing server prefix", uri)
    }

    child, ok := p.findChild(serverName)
    if !ok {
        return nil, fmt.Errorf("unknown server %q", serverName)
    }

    // Parse and strip pagination params if server has paginate enabled
    var pgParams paginate.Params
    if child.Config.Paginate {
        originalURI, pgParams = paginate.ParseParams(originalURI)
    }

    params := protocol.ResourceReadParams{URI: originalURI}

    raw, err := child.Client.Call(ctx, protocol.MethodResourcesRead, params)
    if err != nil {
        return nil, fmt.Errorf("reading resource %s from %s: %w", originalURI, serverName, err)
    }

    var result protocol.ResourceReadResult
    if err := json.Unmarshal(raw, &result); err != nil {
        return nil, fmt.Errorf("decoding resource read result from %s: %w", serverName, err)
    }

    if pgParams.Active {
        result = paginateResourceResult(result, pgParams)
    }

    return &result, nil
}
```

Add the helper function:

``` go
func paginateResourceResult(result protocol.ResourceReadResult, params paginate.Params) protocol.ResourceReadResult {
    for i, content := range result.Contents {
        if content.Text == "" {
            continue
        }
        sliced, err := paginate.SliceArray(content.Text, params)
        if err != nil {
            // Not a JSON array or pagination not active — pass through
            continue
        }
        wrapped, err := json.Marshal(sliced)
        if err != nil {
            continue
        }
        result.Contents[i].Text = string(wrapped)
    }
    return result
}
```

Add `"github.com/amarbel-llc/moxy/internal/paginate"` to the imports.

**Step 3: Run tests to verify nothing is broken**

Run: `go vet ./... && go test ./... -v` Expected: PASS

**Step 4: Commit**

    feat(proxy): wire pagination into ReadResource

--------------------------------------------------------------------------------

### Task 5: Add bats integration tests for pagination

**Files:** - Create: `zz-tests_bats/test-fixtures/resource-server.bash` -
Create: `zz-tests_bats/pagination.bats`

**Step 1: Create the mock resource server**

Create `zz-tests_bats/test-fixtures/resource-server.bash`:

``` bash
#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server that advertises resource capabilities.
# Returns a JSON array of 10 items for resources/read.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
    initialize)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"resources":{}},"serverInfo":{"name":"resource-test","version":"0.1"}}}'
      ;;
    notifications/initialized)
      ;;
    resources/list)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"resources":[{"uri":"test://items","name":"items","mimeType":"application/json"}]}}'
      ;;
    resources/read)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"contents":[{"uri":"test://items","mimeType":"application/json","text":"[1,2,3,4,5,6,7,8,9,10]"}]}}'
      ;;
  esac
done
```

**Step 2: Create the bats test file**

Create `zz-tests_bats/pagination.bats`:

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

function pagination_returns_full_response_without_params { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items"}'
  assert_success
  echo "$output" | jq -e '.contents[0].text == "[1,2,3,4,5,6,7,8,9,10]"'
}

function pagination_slices_with_offset_and_limit { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=0&limit=3"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.total == 10'
  echo "$text" | jq -e '.offset == 0'
  echo "$text" | jq -e '.limit == 3'
  echo "$text" | jq -e '.items == [1,2,3]'
}

function pagination_second_page { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=3&limit=3"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.items == [4,5,6]'
  echo "$text" | jq -e '.total == 10'
}

function pagination_last_page_partial { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=8&limit=5"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.items == [9,10]'
  echo "$text" | jq -e '.total == 10'
}

function pagination_disabled_passes_through { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=0&limit=3"}'
  assert_success
  # Without paginate=true, query params are forwarded as-is (server ignores them)
  # and the full array is returned
  echo "$output" | jq -e '.contents[0].text == "[1,2,3,4,5,6,7,8,9,10]"'
}

function pagination_default_limit { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
paginate = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"res/test://items?offset=0"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.limit == 50'
  # All 10 items returned since 10 < 50
  echo "$text" | jq -e '.items == [1,2,3,4,5,6,7,8,9,10]'
}
```

**Step 3: Run the bats tests**

Run: `just test-bats-file pagination.bats` Expected: PASS (all 6 tests)

**Step 4: Commit**

    test: add bats integration tests for resource pagination

--------------------------------------------------------------------------------

### Task 6: Run full test suite and verify

**Step 1: Run all tests**

Run: `just test` Expected: All Go and bats tests pass.

**Step 2: Verify no regressions**

Run: `go vet ./...` Expected: No issues.
