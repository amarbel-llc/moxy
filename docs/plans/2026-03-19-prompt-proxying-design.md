# Prompt Proxying Design

**Issue:** [#2 — Proxy prompt templates from child servers](https://github.com/amarbel-llc/moxy/issues/2)
**Date:** 2026-03-19

## Problem

Moxy proxies tools and resources from child MCP servers but not prompts.
Clients cannot discover or invoke prompts exposed by child servers.

## Design

Follow the same pattern as tool proxying:

- **Namespacing:** prefix prompt names with `serverName-`, split on first `-`
  for dispatch (same separator as tools)
- **Listing:** `ListPromptsV1` / `ListPrompts` iterate children with
  `Capabilities.Prompts` set, call `prompts/list` via JSON-RPC, decode with
  V1→V0 fallback, prefix each prompt name
- **Getting:** `GetPromptV1` / `GetPrompt` split the incoming name on first `-`
  to extract server and original prompt name, find the child, forward
  `prompts/get`, decode with V1→V0 fallback, return result directly
- **Failed servers:** skipped — no synthetic prompts (tools already expose
  `serverName-status`)
- **Decode helpers:** `decodePromptsList` and `decodePromptGetResult` follow the
  same V1/V0 fallback + upgrade pattern as tools and resources
- **Registration:** `Proxy` registered as `Prompts: p` in `server.New()`, with a
  `PromptProviderV1` type assertion in `main.go`

## Files Modified

- `internal/proxy/proxy.go` — 4 methods (`ListPromptsV1`, `ListPrompts`,
  `GetPromptV1`, `GetPrompt`) + 2 decode helpers
- `cmd/moxy/main.go` — `Prompts: p` option and type assertion

## Testing

- Bats integration test: start moxy with a child that exposes prompts, verify
  `prompts/list` returns prefixed names and `prompts/get` dispatches correctly
