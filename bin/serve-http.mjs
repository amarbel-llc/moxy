#!/usr/bin/env zx

// Start moxy in Streamable HTTP mode, wait for readiness, then launch
// an interactive clown session. Moxy is killed when clown exits.
//
// Usage:
//   zx bin/serve-http.mjs
//   zx bin/serve-http.mjs --port 9090
//   just serve-http
//   just serve-http 9090

import { spawn } from 'node:child_process'
import { createWriteStream } from 'node:fs'

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const MOXY_BIN = path.join(REPO_ROOT, 'build', 'moxy')
const PORT = argv.port ?? '8080'
const LOG_FILE = path.join(REPO_ROOT, 'build', 'serve-http.log')

const logStream = createWriteStream(LOG_FILE, { flags: 'w' })

const moxy = spawn(MOXY_BIN, ['serve', 'mcp'], {
  stdio: ['ignore', 'inherit', logStream],
  env: { ...process.env, MOXY_HTTP_ADDR: `:${PORT}` },
})

process.on('exit', () => moxy.kill())

const url = `http://localhost:${PORT}/healthz`
let ready = false
for (let i = 0; i < 30; i++) {
  try {
    const resp = await fetch(url)
    if (resp.ok) { ready = true; break }
  } catch {
    // not ready yet
  }
  await sleep(500)
}

if (!ready) {
  console.error(`moxy failed to start on :${PORT}`)
  moxy.kill()
  process.exit(1)
}

console.log(`moxy listening on :${PORT} (log: ${LOG_FILE}), starting clown...`)

try {
  const clown = spawn('clown', [], { stdio: 'inherit' })
  const code = await new Promise((resolve) => {
    clown.on('exit', (code) => resolve(code ?? 0))
    clown.on('error', () => resolve(1))
  })
  process.exit(code)
} finally {
  moxy.kill()
}
