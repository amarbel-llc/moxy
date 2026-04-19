#!/usr/bin/env zx

// Test moxin loading by bootstrapping a temp workspace with the local build,
// running a JSON-RPC initialize + tools/list handshake, and printing the
// moxin.log trace.
//
// Usage:
//   zx bin/test-moxin-loading.mjs
//
// Environment setup:
//   - Builds moxy + moxins from the working tree
//   - Keeps real HOME (global moxyfile servers may fail to connect — that's fine)
//   - Sets MOXIN_PATH to build/moxins
//   - CWD is a temp dir outside the moxyfile hierarchy
//
// This reproduces the environment a non-moxy repo (e.g. madder) would see.

import { spawn } from 'node:child_process'
import { homedir } from 'node:os'

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const MOXY_BIN = path.join(REPO_ROOT, 'build', 'moxy')
const MOXINS_DIR = path.join(REPO_ROOT, 'result', 'share', 'moxy', 'moxins')

// --- Build ---

console.log('Building moxy and moxins...')
try {
  await $`just -f ${path.join(REPO_ROOT, 'justfile')} build-go`.pipe(process.stderr)
} catch (e) {
  console.error('Build failed:', e.message)
  process.exit(1)
}

if (!(await fs.pathExists(MOXY_BIN))) {
  console.error(`error: moxy binary not found at ${MOXY_BIN}`)
  process.exit(1)
}

// --- Moxin log ---

const logHome = process.env.XDG_LOG_HOME || path.join(homedir(), '.local', 'log')
const MOXIN_LOG = path.join(logHome, 'moxy', 'moxin.log')

// Truncate the log so we only see entries from this run.
await fs.writeFile(MOXIN_LOG, '').catch(() => {})

// --- Create temp workspace ---

const WORK_DIR = await fs.mkdtemp(path.join(REPO_ROOT, '.moxin-test-'))

let cleaningUp = false
async function cleanup() {
  if (cleaningUp) return
  cleaningUp = true
  await fs.remove(WORK_DIR).catch(() => {})
}

let signalReceived = false
async function handleSignal(code) {
  if (signalReceived) return
  signalReceived = true
  await cleanup()
  process.exit(code)
}

process.on('SIGINT', () => handleSignal(130))
process.on('SIGTERM', () => handleSignal(143))

// --- Run moxy with JSON-RPC handshake ---

console.log('')
console.log('Running moxy initialize + tools/list handshake...')
console.log(`  moxy binary: ${MOXY_BIN}`)
console.log(`  moxins dir:  ${MOXINS_DIR}`)
console.log(`  work dir:    ${WORK_DIR}`)
console.log('')

const initRequest = JSON.stringify({
  jsonrpc: '2.0',
  id: 1,
  method: 'initialize',
  params: {
    protocolVersion: '2025-03-26',
    capabilities: {},
    clientInfo: { name: 'test-moxin-loading', version: '0.0.1' },
  },
})

const toolsListRequest = JSON.stringify({
  jsonrpc: '2.0',
  id: 2,
  method: 'tools/list',
  params: {},
})

const input = initRequest + '\n' + toolsListRequest + '\n'

let stdout = ''
let stderr = ''

// Use the serve-mcp subcommand explicitly: invoking moxy with no args prints
// the CLI help and exits before runServer, so the MCP handshake would never
// happen and this test would silently report 0 tools.
const moxyChild = spawn(MOXY_BIN, ['serve-mcp'], {
  cwd: WORK_DIR,
  env: {
    ...process.env,
    MOXIN_PATH: MOXINS_DIR,
  },
  stdio: ['pipe', 'pipe', 'pipe'],
})

moxyChild.stdout.on('data', (chunk) => {
  stdout += chunk.toString()
})

moxyChild.stderr.on('data', (chunk) => {
  stderr += chunk.toString()
})

moxyChild.stdin.write(input)
moxyChild.stdin.end()

await new Promise((resolve) => {
  moxyChild.on('exit', () => resolve())
  moxyChild.on('error', () => resolve())
})

// --- Parse and display results ---

console.log('=== moxy stderr ===')
console.log(stderr || '(empty)')
console.log('')

// Parse tools/list response to show discovered tools.
const responses = stdout
  .trim()
  .split('\n')
  .filter((l) => l.trim())

let toolNames = []
for (const line of responses) {
  try {
    const msg = JSON.parse(line)
    if (msg.id === 2 && msg.result?.tools) {
      toolNames = msg.result.tools.map((t) => t.name).sort()
    }
  } catch {}
}

console.log(`=== tools/list: ${toolNames.length} tools ===`)

// Group by server prefix.
const byServer = {}
for (const name of toolNames) {
  const dot = name.indexOf('.')
  const server = dot > 0 ? name.slice(0, dot) : '(no prefix)'
  if (!byServer[server]) byServer[server] = []
  byServer[server].push(name)
}

for (const [server, tools] of Object.entries(byServer).sort()) {
  console.log(`  ${server}: ${tools.length} tools`)
}

console.log('')
console.log('=== moxin.log ===')

if (await fs.pathExists(MOXIN_LOG)) {
  const log = await fs.readFile(MOXIN_LOG, 'utf8')
  console.log(log || '(empty)')
} else {
  console.log('(file does not exist)')
}

await cleanup()

if (toolNames.length === 0) {
  console.error('FAIL: no tools discovered — handshake or moxin probe failed')
  process.exit(1)
}
