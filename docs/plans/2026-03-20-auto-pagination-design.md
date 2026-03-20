# Auto-Pagination for Large Resource Responses

GitHub issue: #5

## Problem

MCP resources can return large JSON arrays (e.g., a CalDAV calendar with 1079
tasks at 301KB). This blows up agent context windows. Agents need a way to
paginate these responses so a top-level agent can shard pages across subagents.

## Design

### Opt-in Configuration

New optional `paginate` field on `[[servers]]` in the moxyfile:

``` toml
[[servers]]
name = "caldav"
command = "caldav-mcp"
paginate = true
```

Only servers with `paginate = true` get pagination behavior. Others pass through
unchanged. This avoids interfering with servers that implement their own
pagination.

### Resource Read Flow

When moxy receives a `resources/read` for a paginate-enabled server:

1.  Parse and strip `?offset=N&limit=M` query parameters from the URI before
    forwarding to the child server.
2.  Forward the clean URI to the child, get the full response.
3.  If the response content is a JSON array and pagination params were present,
    slice the array and wrap the response.
4.  If no pagination params are present, return the full response unchanged
    (backward compatible).
5.  If pagination params are present but the content is not a JSON array, return
    the full response unchanged (pass-through, no error).

Default limit is 50 when `?offset` is present but `?limit` is missing.

### Response Format

Paginated responses wrap the array slice:

``` json
{
  "items": [ ... ],
  "total": 1079,
  "offset": 0,
  "limit": 50
}
```

This replaces the raw JSON array in the resource content block's `text` field.
The `mimeType` stays `application/json`.

The `total` field lets agents calculate the number of pages for parallel
dispatch.

### Scope Constraints

- JSON arrays only. Other content types pass through unchanged.
- No list-time annotations (item counts on `resources/list`). May add later.
- No moxyfile-level page size config. May add a `page_size` lever in the future.

### Rollback

Purely additive and opt-in. Rollback: remove `paginate = true` from the moxyfile
entry.
