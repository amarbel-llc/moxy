#!/usr/bin/env zx

// Start moxy in streamable-HTTP mode on an OS-assigned ephemeral port via
// the clown-plugin-protocol handshake (man clown-plugin-protocol(7)),
// generate a project-scoped `.mcp.json` pointing at it, and drop into the
// user's $SHELL so they can launch `claude` (or any MCP client) themselves.
//
// Usage:
//   zx bin/serve-http.mjs
//   just serve-http
//
// Lifecycle: when the spawned shell exits, moxy is killed and the
// generated .mcp.json is removed.

import { spawn } from 'node:child_process'
import { openSync } from 'node:fs'

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const MOXY_BIN = path.join(REPO_ROOT, 'build', 'moxy')
const TMP_DIR = path.join(REPO_ROOT, '.tmp')
const MCP_JSON = path.join(TMP_DIR, '.mcp.json')
const LOG_FILE = path.join(REPO_ROOT, 'build', 'serve-http.log')

await fs.ensureDir(TMP_DIR)
await fs.ensureDir(path.dirname(LOG_FILE))

const logFd = openSync(LOG_FILE, 'w')

const moxy = spawn(MOXY_BIN, ['serve-http'], {
  stdio: ['ignore', 'pipe', logFd],
  env: process.env,
})

process.on('exit', () => {
  try { moxy.kill() } catch {}
  try { fs.removeSync(MCP_JSON) } catch {}
})

// Parse the first stdout line: "1|1|tcp|<host:port>|streamable-http"
// (man clown-plugin-protocol(7) — HANDSHAKE PROTOCOL).
const addr = await new Promise((resolve, reject) => {
  let buf = ''
  const timer = setTimeout(() => {
    moxy.stdout.off('data', onData)
    moxy.off('exit', onExit)
    reject(new Error(`moxy handshake timeout; see ${LOG_FILE}`))
  }, 10_000)
  const settle = (fn, value) => {
    clearTimeout(timer)
    moxy.stdout.off('data', onData)
    moxy.off('exit', onExit)
    fn(value)
  }
  const onData = (chunk) => {
    buf += chunk.toString('utf8')
    const nl = buf.indexOf('\n')
    if (nl < 0) return
    const line = buf.slice(0, nl)
    const parts = line.split('|')
    if (parts.length < 5 || parts[0] !== '1' || parts[2] !== 'tcp') {
      settle(reject, new Error(`unexpected handshake line from moxy: ${line}`))
      return
    }
    settle(resolve, parts[3])
  }
  const onExit = (code) => settle(reject, new Error(`moxy exited before handshake (code ${code}); see ${LOG_FILE}`))
  moxy.stdout.on('data', onData)
  moxy.on('exit', onExit)
})

// /healthz readiness gate.
const healthURL = `http://${addr}/healthz`
let ready = false
for (let i = 0; i < 60; i++) {
  try {
    const resp = await fetch(healthURL)
    if (resp.ok) { ready = true; break }
  } catch {
    // not ready yet
  }
  await sleep(100)
}
if (!ready) {
  console.error(`moxy /healthz never became 200 at ${healthURL}; see ${LOG_FILE}`)
  moxy.kill()
  process.exit(1)
}

const mcpURL = `http://${addr}/mcp`
const mcpConfig = {
  mcpServers: {
    moxy: { type: 'http', url: mcpURL },
  },
}
await fs.writeFile(MCP_JSON, JSON.stringify(mcpConfig, null, 2) + '\n')

console.log('')
console.log(`moxy listening on ${addr}`)
console.log(`  log:        ${LOG_FILE}`)
console.log(`  .mcp.json:  ${MCP_JSON}`)
console.log('')
console.log('Launch Claude Code pointed at this config:')
console.log(`  claude --mcp-config ${MCP_JSON}`)
console.log('')
console.log('Exit the shell to stop moxy and remove .mcp.json.')
console.log('')

const shell = process.env.SHELL ?? '/bin/bash'
const shellChild = spawn(shell, [], {
  stdio: 'inherit',
  cwd: REPO_ROOT,
  env: { ...process.env, MOXY_DEV_MCP_JSON: MCP_JSON },
})

const exitCode = await new Promise((resolve) => {
  shellChild.on('exit', (code) => resolve(code ?? 0))
  shellChild.on('error', () => resolve(1))
})

moxy.kill()
try { await fs.remove(MCP_JSON) } catch {}

process.exit(exitCode)
