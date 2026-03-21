# Synthetic Resource Tools + Snob-Case Tool Names

**Date:** 2026-03-21 **Status:** approved

## Problem

1.  MCP resources are only accessible via protocol-level methods
    (`resources/read`, `resources/templates/list`), not as tools. Subagents
    spawned by Claude Code can only call tools, so they cannot discover or read
    resources from child servers.

2.  Tool names use hyphens internally (e.g., `lux-execute-command`), making the
    server-name separator ambiguous --- `splitPrefix` splits on the first `-`,
    but tool names with hyphens create confusion.

## Solution

### Snob-case conversion

Convert all child tool and prompt names to snob-case: hyphens within the
original name become underscores, so the only `-` is the server separator.

- `execute-command` from `lux` → `lux-execute_command`
- `resource-read` from `grit` → `grit-resource_read`

**ListToolsV1 / ListPromptsV1:** apply `strings.ReplaceAll(name, "-", "_")` to
child tool/prompt names after prefixing with server name.

**CallToolV1 / GetPromptV1:** after `splitPrefix`, reverse the conversion
(`strings.ReplaceAll(toolName, "_", "-")`) before forwarding to the child.

This is a breaking change --- no dual-architecture since the old naming was
inherently ambiguous.

### Synthetic resource tools

For each child server that declares resource capabilities
(`Capabilities.Resources != nil`), auto-generate two tools:

**`<server>-resource_read`** - Description:
`"Read a resource from <server> by URI"` - Input schema:
`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI"}},"required":["uri"]}` -
Implementation: calls `resources/read` on the child, returns the result as tool
content

**`<server>-resource_templates`** - Description:
`"List available resource templates for <server>"` - Input schema:
`{"type":"object"}` - Implementation: calls `resources/templates/list` on the
child, returns the template list as JSON

**Collision handling:** if a child already exposes a tool whose snob-cased name
is `resource_read` or `resource_templates`, the child's tool takes precedence
--- no synthetic tool is generated.

**Opt-out:** a moxyfile field `resource_tools = false` on a server entry
suppresses synthetic tool generation for that server. Default is true.

### Config change

Add `ResourceTools *bool` to `ServerConfig`:

``` toml
[servers.grit]
command = "grit"
resource_tools = false  # suppress synthetic resource tools
```

`nil` (absent) and `true` both enable synthetic tools. Only explicit `false`
disables.

## Files to modify

1.  `internal/proxy/proxy.go` --- snob-case in
    ListToolsV1/CallToolV1/ListPromptsV1/GetPromptV1, synthetic tool injection,
    resource_read/resource_templates call handling
2.  `internal/config/config.go` --- `ResourceTools` field
3.  `internal/config/config_test.go` --- parse test for `resource_tools`
4.  `zz-tests_bats/` --- new bats tests for snob-case and synthetic resource
    tools

## Testing

- Go unit tests: snob-case conversion, synthetic tool generation, collision
  detection, config parsing for `resource_tools`
- Bats integration tests:
  - Tool names use snob-case in `tools/list`
  - `resource_read` and `resource_templates` tools appear for resource-capable
    servers
  - `resource_read` actually reads a resource via the child
  - `resource_tools = false` suppresses synthetic tools
  - No synthetic tool when child already provides `resource-read` /
    `resource_read`
- Deferred: `purse-first validate-mcp` recipe once purse-first#1 ships (moxy#7)

## Related issues

- moxy#6 --- facility for top-level agents to instruct subagents on MCP usage
- moxy#7 --- add validate-mcp just recipe once purse-first ships it
