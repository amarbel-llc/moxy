package moxy

import (
	"encoding/json"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
	"code.linenisgreat.com/purse-first/libs/go-mcp/server"

	"code.linenisgreat.com/moxy/internal/asyncjob"
	"code.linenisgreat.com/moxy/internal/proxy"
)

func boolPtr(b bool) *bool { return &b }

// registerMetaTools registers moxy's own tools — restart, batch, async,
// async-result, async-cancel — on registry, dispatching to p. The
// descriptions are the ones the binary has always advertised.
func registerMetaTools(registry *server.ToolRegistryV1, p *proxy.Proxy, asyncManager *asyncjob.Manager) {
	registry.Register(
		protocol.ToolV1{
			Name:        "restart",
			Description: "Restart a configured child server or moxin by name. Omit `server` to reload the moxyfile hierarchy and MOXIN_PATH and rebuild every running child. Permission-gated (destructive). Note: Claude Code does not refresh the active conversation's tool list on notifications/tools/list_changed; follow with /mcp -> Reconnect to update the system prompt. See moxy-restart(7).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Server or moxin name. Omit to reload everything."}}}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:    boolPtr(false),
				DestructiveHint: boolPtr(true),
			},
		},
		p.HandleRestart,
	)
	registry.Register(
		protocol.ToolV1{
			Name:        "batch",
			Description: "Run a sequence of moxin sub-calls under a single permission prompt. Each sub-call must resolve to allow or ask via moxy's perms-request system; deny or unknown aborts the batch. Output is TAP-NDJSON. See moxy-batch(7). With async=true the whole batch backgrounds as ONE async job (allow-only preflight; the agent is woken on completion and fetches the TAP-NDJSON via async-result).",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["calls"],
				"properties":{
					"calls":{
						"type":"array",
						"minItems":1,
						"items":{
							"type":"object",
							"required":["tool"],
							"properties":{
								"tool":{"type":"string","description":"Namespaced tool name (e.g. grit.tag)"},
								"args":{"type":"object","description":"Sub-call arguments"}
							}
						}
					},
					"on_error":{"type":"string","enum":["stop","continue"],"default":"stop"},
					"async":{"type":"boolean","default":false,"description":"Background the batch as one async job; every sub-call must resolve to allow"}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:    boolPtr(false),
				DestructiveHint: boolPtr(true),
			},
		},
		p.HandleBatch,
	)

	// Async dispatch (FDR 0004): results go to the user-level madder store so
	// they survive worktrees and sessions. The store is provisioned by
	// home-manager — moxy only writes, NEVER creates (madder#227: init from
	// inside a worktree lands in the ancestor .madder, shadowing XDG scope).
	// Until the store exists, async degrades gracefully: jobs still reach
	// terminal states; only the digest is missing from wake messages.
	p.SetAsyncManager(asyncManager)
	registry.Register(
		protocol.ToolV1{
			Name:        "async",
			Description: "Dispatch one tool call in the background. Returns {job_id, status:\"running\"} immediately; when the call reaches a terminal state the agent is woken via clown's job-wakeup channel with a summary and the result blob's madder://blobs/<digest> reference. Fetch the result with async-result. The returned job_id is a real clown ringmaster job — if you have the ringmaster job tools, ringmaster's job_wait blocks on this job_id until it terminalizes (and job_status reports it with source \"moxy\"); prefer that over hot-polling async-result. Only calls whose permission resolves to allow may background (ask/deny/unknown are rejected synchronously). The optional per-call `timeout` defaults to 30 minutes when omitted; a larger value is honored — there is no hard ceiling. On timeout the whole process tree is killed and the job terminalizes with status `timeout`. See docs/features/0004-async-tool-dispatch.md.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["tool"],
				"properties":{
					"tool":{"type":"string","description":"Namespaced tool name (e.g. rg.search)"},
					"args":{"type":"object","description":"Arguments for the tool call"},
					"timeout":{"type":"string","description":"Max wall-clock duration before the job's whole process tree is killed and it terminalizes with status \"timeout\" (e.g. \"10m\", \"90s\"). Defaults to 30m when omitted; a larger value is honored — there is no hard ceiling."}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:    boolPtr(false),
				DestructiveHint: boolPtr(true),
			},
		},
		p.HandleAsync,
	)
	registry.Register(
		protocol.ToolV1{
			Name:        "async-result",
			Description: "Fetch an async job's status, or its full stored tool result once terminal. Reads state from clown's job journal, so it resolves jobs launched by another session too, falling back to this process's in-memory index when the journal is unavailable. For a running job it also surfaces live progress when the clown output spool is available — elapsed_sec, last_activity, spool_bytes, and a bounded output tail — so you can tell a working job from a wedged one without waiting for the terminal wake. This is poll-only: to block until a running job finishes, use ringmaster's job_wait on the job_id (a moxy async job is a real ringmaster job) rather than looping on async-result. Doubles as the poll surface when job wakeups are disabled (CLOWN_DISABLE_JOB_WAKEUP=1).",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["job_id"],
				"properties":{
					"job_id":{"type":"string","description":"Job id returned by async"}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:   boolPtr(true),
				IdempotentHint: boolPtr(true),
			},
		},
		p.HandleAsyncResult,
	)
	registry.Register(
		protocol.ToolV1{
			Name:        "async-cancel",
			Description: "Cancel a running async job. The job reaches terminal state `cancelled` and wakes the agent like any other terminal state. Cancelling an already-terminal job is a no-op reporting that state.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"required":["job_id"],
				"properties":{
					"job_id":{"type":"string","description":"Job id returned by async"}
				}
			}`),
			Annotations: &protocol.ToolAnnotations{
				ReadOnlyHint:   boolPtr(false),
				IdempotentHint: boolPtr(true),
			},
		},
		p.HandleAsyncCancel,
	)
}
