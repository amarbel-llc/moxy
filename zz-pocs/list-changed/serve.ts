// POC: minimal streamable-HTTP MCP server with a `flip` tool that swaps the
// advertised tool surface and broadcasts `notifications/tools/list_changed`.
//
// Goal: isolate Claude Code's behavior on `list_changed` from moxy. After
// the flip, does the next user-turn's tool registry reflect the new set, or
// does Claude only refresh on session restart?
//
// Wire format mirrors moxy/internal/streamhttp/server.go:
//   POST /mcp        — JSON-RPC requests/responses; initialize returns
//                      Mcp-Session-Id header.
//   GET  /mcp        — SSE stream (requires Mcp-Session-Id header).
//                      Server pushes `data: <json>\n\n` events.
//   DELETE /mcp      — ends the session.
//   /healthz         — 200 OK.
//
// Stdout: prints the clown-plugin-protocol handshake line as moxy does:
//   1|1|tcp|<addr>|streamable-http\n
//
// Trigger: Claude calls the `flip` tool. The tool swaps the active tool set
// and broadcasts list_changed to all open SSE streams. The current turn
// will see the flip's "ok" response; the *next* turn is the test — does
// Claude know the new tool surface?

import { randomUUID } from 'node:crypto'

type ToolDef = {
  name: string
  description: string
  inputSchema: { type: 'object'; properties?: Record<string, unknown>; required?: string[] }
}

type ToolHandler = (args: Record<string, unknown>) => string

// Two starkly different tool sets. Pre-flip exposes `alpha-only`; post-flip
// exposes `beta-only`. Both also expose `flip` and `state` (status probe)
// so the test driver always has a way to introspect.
const COMMON_TOOLS: Array<{ def: ToolDef; handler: ToolHandler }> = [
  {
    def: {
      name: 'flip',
      description:
        'Swap the advertised tool set between alpha and beta and broadcast notifications/tools/list_changed.',
      inputSchema: { type: 'object', properties: {} },
    },
    handler: () => {
      const before = state
      flipState()
      return `flipped ${before} -> ${state}`
    },
  },
  {
    def: {
      name: 'state',
      description: 'Report the currently advertised tool set (alpha or beta).',
      inputSchema: { type: 'object', properties: {} },
    },
    handler: () => `current state: ${state}`,
  },
]

const ALPHA_ONLY: Array<{ def: ToolDef; handler: ToolHandler }> = [
  {
    def: {
      name: 'alpha-only',
      description:
        'Visible ONLY before the flip. If Claude can call this after a flip, the system prompt was not refreshed.',
      inputSchema: { type: 'object', properties: {} },
    },
    handler: () => 'alpha-only invoked',
  },
]

const BETA_ONLY: Array<{ def: ToolDef; handler: ToolHandler }> = [
  {
    def: {
      name: 'beta-only',
      description:
        'Visible ONLY after the flip. If Claude can call this in the same turn as the flip, list_changed propagated mid-turn.',
      inputSchema: { type: 'object', properties: {} },
    },
    handler: () => 'beta-only invoked',
  },
]

let state: 'alpha' | 'beta' = 'alpha'

function activeTools(): Array<{ def: ToolDef; handler: ToolHandler }> {
  const variant = state === 'alpha' ? ALPHA_ONLY : BETA_ONLY
  return [...COMMON_TOOLS, ...variant]
}

function flipState() {
  state = state === 'alpha' ? 'beta' : 'alpha'
  broadcast({ jsonrpc: '2.0', method: 'notifications/tools/list_changed' })
}

// --- session + SSE state ---

let sessionId: string | null = null
const sseControllers = new Set<ReadableStreamDefaultController<Uint8Array>>()
const encoder = new TextEncoder()

function broadcast(msg: object) {
  const payload = encoder.encode(`data: ${JSON.stringify(msg)}\n\n`)
  for (const c of sseControllers) {
    try { c.enqueue(payload) } catch {}
  }
}

// --- JSON-RPC dispatch ---

type RpcRequest = {
  jsonrpc: '2.0'
  id?: string | number | null
  method: string
  params?: unknown
}

function rpcResult(id: unknown, result: unknown) {
  return { jsonrpc: '2.0', id, result }
}
function rpcError(id: unknown, code: number, message: string) {
  return { jsonrpc: '2.0', id, error: { code, message } }
}

function dispatch(msg: RpcRequest): object | null {
  switch (msg.method) {
    case 'initialize':
      return rpcResult(msg.id, {
        protocolVersion: '2025-11-25',
        serverInfo: { name: 'list-changed-poc', version: '0.1.0' },
        capabilities: { tools: { listChanged: true } },
        instructions:
          'POC server. Call `flip` to swap between alpha and beta tool sets. ' +
          'After flip, alpha-only/beta-only swap visibility via tools/list_changed.',
      })

    case 'notifications/initialized':
      return null

    case 'tools/list': {
      const tools = activeTools().map((t) => t.def)
      return rpcResult(msg.id, { tools })
    }

    case 'tools/call': {
      const params = (msg.params ?? {}) as { name?: string; arguments?: Record<string, unknown> }
      const tool = activeTools().find((t) => t.def.name === params.name)
      if (!tool) {
        return rpcResult(msg.id, {
          isError: true,
          content: [{ type: 'text', text: `unknown tool: ${params.name}` }],
        })
      }
      const text = tool.handler(params.arguments ?? {})
      return rpcResult(msg.id, { content: [{ type: 'text', text }] })
    }

    case 'resources/list':
      return rpcResult(msg.id, { resources: [] })
    case 'resources/templates/list':
      return rpcResult(msg.id, { resourceTemplates: [] })
    case 'prompts/list':
      return rpcResult(msg.id, { prompts: [] })

    default:
      return rpcError(msg.id, -32601, `method not found: ${msg.method}`)
  }
}

// --- HTTP transport ---

const server = Bun.serve({
  hostname: '127.0.0.1',
  port: 0,
  async fetch(req) {
    const url = new URL(req.url)

    if (url.pathname === '/healthz') {
      return new Response('ok', { status: 200 })
    }
    if (url.pathname !== '/mcp' && url.pathname !== '/') {
      return new Response('not found', { status: 404 })
    }

    if (req.method === 'POST') {
      const body = await req.text()
      let msg: RpcRequest
      try { msg = JSON.parse(body) } catch {
        return Response.json(rpcError(null, -32700, 'parse error'), { status: 200 })
      }

      if (msg.method === 'initialize') {
        const id = sessionId ?? (sessionId = randomUUID())
        const resp = dispatch(msg)
        return new Response(JSON.stringify(resp), {
          status: 200,
          headers: { 'Content-Type': 'application/json', 'Mcp-Session-Id': id },
        })
      }

      const sid = req.headers.get('Mcp-Session-Id')
      if (!sessionId) return new Response('no active session', { status: 404 })
      if (!sid) return new Response('missing Mcp-Session-Id header', { status: 400 })
      if (sid !== sessionId) return new Response('invalid session', { status: 404 })

      const isNotification = msg.id === undefined || msg.id === null
      if (isNotification) {
        dispatch(msg)
        return new Response(null, { status: 202 })
      }

      const resp = dispatch(msg)
      if (resp == null) return new Response(null, { status: 202 })
      return Response.json(resp, { status: 200 })
    }

    if (req.method === 'GET') {
      const sid = req.headers.get('Mcp-Session-Id')
      if (!sessionId) return new Response('no active session', { status: 404 })
      if (sid !== sessionId) return new Response('invalid session', { status: 404 })

      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          sseControllers.add(controller)
        },
        cancel() {
          // Controller already enters cancelled state; just drop it from the set.
        },
      })
      // Prune disconnected controllers when the request signals abort.
      const ac = req.signal
      ac.addEventListener('abort', () => {
        for (const c of sseControllers) {
          try { c.close() } catch {}
        }
        sseControllers.clear()
      }, { once: true })

      return new Response(stream, {
        status: 200,
        headers: {
          'Content-Type': 'text/event-stream',
          'Cache-Control': 'no-cache',
          Connection: 'keep-alive',
        },
      })
    }

    if (req.method === 'DELETE') {
      sessionId = null
      for (const c of sseControllers) {
        try { c.close() } catch {}
      }
      sseControllers.clear()
      return new Response('ok', { status: 200 })
    }

    return new Response('method not allowed', { status: 405 })
  },
})

// clown-plugin-protocol handshake (man clown-plugin-protocol(7) §HANDSHAKE PROTOCOL).
const addr = `${server.hostname}:${server.port}`
process.stdout.write(`1|1|tcp|${addr}|streamable-http\n`)
process.stderr.write(`list-changed POC: serving streamable-http on ${addr}\n`)

// Keep alive on SIGINT/SIGTERM.
for (const sig of ['SIGINT', 'SIGTERM'] as const) {
  process.on(sig, () => {
    server.stop(true)
    process.exit(0)
  })
}
