#!/usr/bin/env zx

// Tests the stderrlog per-session logging + rotation flow.
//
// Usage:
//   zx bin/test-stderrlog.mjs
//
// Phases:
//   1. Clean lifecycle: spawn → init handshake → close stdin → rotation to completed/.
//   2. Kill mid-run: spawn → SIGKILL → leaves file in active/.
//   3. Orphan sweep: next moxy startup → dead-pid file moves to completed/…orphan.log.
//   4. SessionEnd hook: invoke `moxy hook` with SessionEnd payload → active/<id>.log
//      moves to completed/<id>.log.
//
// An isolated XDG_LOG_HOME temp dir is used throughout so no real user logs
// are touched.

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

// --- Isolated log home ---

const LOG_HOME = await fs.mkdtemp(path.join(REPO_ROOT, '.stderrlog-test-'))
const STDERR_ROOT = path.join(LOG_HOME, 'moxy', 'stderr')

let cleaningUp = false
async function cleanup() {
  if (cleaningUp) return
  cleaningUp = true
  await fs.remove(LOG_HOME).catch(() => {})
}
process.on('SIGINT', async () => { await cleanup(); process.exit(130) })
process.on('SIGTERM', async () => { await cleanup(); process.exit(143) })

// --- Helpers ---

function sanitizeSessionID(id) {
  // Mirrors stderrlog.sanitize: slash preserved, non-[A-Za-z0-9/._-] → _,
  // ".." → _, leading/trailing slashes stripped.
  let out = ''
  for (const ch of id) {
    if (/[A-Za-z0-9/._\-]/.test(ch)) out += ch
    else out += '_'
  }
  out = out.replace(/\.\./g, '_').replace(/^\/+|\/+$/g, '')
  return out
}

function activePath(sessionID) {
  return path.join(STDERR_ROOT, 'active', sanitizeSessionID(sessionID) + '.log')
}

function completedPath(sessionID) {
  return path.join(STDERR_ROOT, 'completed', sanitizeSessionID(sessionID) + '.log')
}

function orphanPath(sessionID) {
  return path.join(STDERR_ROOT, 'completed', sanitizeSessionID(sessionID) + '.orphan.log')
}

function spawnMoxy(sessionID, { extraEnv = {} } = {}) {
  // Use the serve-mcp subcommand explicitly; without args moxy prints help
  // and exits without entering runServer (so stderrlog.Init never runs).
  return spawn(MOXY_BIN, ['serve-mcp'], {
    cwd: LOG_HOME, // outside any moxyfile hierarchy
    env: {
      ...process.env,
      XDG_LOG_HOME: LOG_HOME,
      MOXIN_PATH: MOXINS_DIR,
      SPINCLASS_SESSION_ID: sessionID,
      ...extraEnv,
    },
    stdio: ['pipe', 'pipe', 'pipe'],
  })
}

function initHandshake() {
  const init = JSON.stringify({
    jsonrpc: '2.0',
    id: 1,
    method: 'initialize',
    params: {
      protocolVersion: '2025-03-26',
      capabilities: {},
      clientInfo: { name: 'test-stderrlog', version: '0.0.1' },
    },
  })
  return init + '\n'
}

async function waitForFile(p, timeoutMs = 5000) {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    if (await fs.pathExists(p)) return true
    await new Promise((r) => setTimeout(r, 50))
  }
  return false
}

function assert(cond, msg) {
  if (!cond) {
    console.error(`  ✗ FAIL: ${msg}`)
    process.exitCode = 1
    return false
  }
  console.log(`  ✓ ${msg}`)
  return true
}

async function listTree(dir, prefix = '') {
  if (!(await fs.pathExists(dir))) return `${prefix}(missing: ${dir})`
  const entries = await fs.readdir(dir, { withFileTypes: true })
  const lines = []
  for (const e of entries) {
    const p = path.join(dir, e.name)
    lines.push(`${prefix}${e.name}${e.isDirectory() ? '/' : ''}`)
    if (e.isDirectory()) lines.push(await listTree(p, prefix + '  '))
  }
  return lines.filter(Boolean).join('\n')
}

// --- Phase 1: clean lifecycle ---

console.log('\n=== Phase 1: clean lifecycle (init → stdin close → rotate) ===')
{
  const SESSION = 'test-stderrlog/clean'
  const child = spawnMoxy(SESSION)

  let stdout = ''
  let stderrBuf = ''
  child.stdout.on('data', (c) => { stdout += c.toString() })
  child.stderr.on('data', (c) => { stderrBuf += c.toString() })
  child.stdin.write(initHandshake())

  // Wait until moxy has written the banner to the active file before closing.
  const appeared = await waitForFile(activePath(SESSION))
  if (!appeared) {
    console.log(`  (diag) expected: ${activePath(SESSION)}`)
    console.log(`  (diag) STDERR_ROOT tree:\n    ${(await listTree(STDERR_ROOT, '    ')) || '    (no stderr root yet)'}`)
    console.log(`  (diag) LOG_HOME tree:\n${await listTree(LOG_HOME, '    ')}`)
    console.log(`  (diag) moxy stderr:\n    ${stderrBuf.split('\n').join('\n    ')}`)
    console.log(`  (diag) moxy stdout (first 500 chars):\n    ${stdout.slice(0, 500)}`)
    console.log(`  (diag) child.exitCode=${child.exitCode} child.signalCode=${child.signalCode}`)
  }
  assert(await fs.pathExists(activePath(SESSION)), 'active/<id>.log exists during run')

  child.stdin.end()
  await new Promise((resolve) => child.on('exit', resolve))

  const bannerContent = await fs.readFile(completedPath(SESSION), 'utf8').catch(() => '')
  assert(
    !(await fs.pathExists(activePath(SESSION))),
    'active/<id>.log is gone after clean shutdown',
  )
  assert(
    await fs.pathExists(completedPath(SESSION)),
    'completed/<id>.log exists after rotation',
  )
  assert(bannerContent.includes('--- moxy started'), 'banner line present in rotated file')
  assert(bannerContent.includes('--- moxy stopping'), 'shutdown line present in rotated file')
}

// --- Phase 2: kill mid-run ---

console.log('\n=== Phase 2: kill mid-run (SIGKILL) leaves file in active/ ===')
const KILLED_SESSION = 'test-stderrlog/killed'
let killedPid = null
{
  const child = spawnMoxy(KILLED_SESSION)
  killedPid = child.pid
  child.stdin.write(initHandshake())

  const appeared = await waitForFile(activePath(KILLED_SESSION))
  assert(appeared, 'active/<id>.log exists mid-run')

  child.kill('SIGKILL')
  await new Promise((resolve) => child.on('exit', resolve))

  assert(
    await fs.pathExists(activePath(KILLED_SESSION)),
    'active/<id>.log remains after SIGKILL (no rotation)',
  )
  assert(
    await fs.pathExists(activePath(KILLED_SESSION) + '.pid'),
    '.pid sidecar remains after SIGKILL',
  )
}

// --- Phase 3: orphan sweep on next startup ---

console.log('\n=== Phase 3: next startup sweeps dead-pid file to orphan.log ===')
{
  // Sanity: killedPid should no longer exist on this host by now.
  const stillAlive = await fs.pathExists(`/proc/${killedPid}`)
  if (stillAlive) {
    console.log(`  (note: pid ${killedPid} still visible in /proc — skipping orphan sweep check)`)
  } else {
    const SESSION = 'test-stderrlog/sweeper'
    const child = spawnMoxy(SESSION)
    child.stdin.write(initHandshake())
    await waitForFile(activePath(SESSION))

    assert(
      !(await fs.pathExists(activePath(KILLED_SESSION))),
      'killed session no longer in active/',
    )
    assert(
      await fs.pathExists(orphanPath(KILLED_SESSION)),
      'killed session moved to completed/<id>.orphan.log',
    )

    child.stdin.end()
    await new Promise((resolve) => child.on('exit', resolve))
  }
}

// --- Phase 4: SessionEnd hook rotates ---

console.log('\n=== Phase 4: SessionEnd hook rotates active → completed ===')
{
  const SESSION = 'test-stderrlog/hook'
  const child = spawnMoxy(SESSION)
  child.stdin.write(initHandshake())
  await waitForFile(activePath(SESSION))
  assert(await fs.pathExists(activePath(SESSION)), 'active/<id>.log exists before hook')

  // Simulate what Claude Code does on SessionEnd — invoke `moxy hook` in a
  // separate process with the event payload on stdin.
  const hookPayload = JSON.stringify({
    hook_event_name: 'SessionEnd',
    session_id: 'does-not-matter',
  })
  await $({
    stdio: ['pipe', 'inherit', 'inherit'],
    input: hookPayload,
    env: { ...process.env, XDG_LOG_HOME: LOG_HOME, SPINCLASS_SESSION_ID: SESSION },
  })`${MOXY_BIN} hook`

  assert(
    !(await fs.pathExists(activePath(SESSION))),
    'active/<id>.log gone after SessionEnd hook',
  )
  assert(
    await fs.pathExists(completedPath(SESSION)),
    'completed/<id>.log exists after SessionEnd hook',
  )

  // Clean up the still-running moxy process.
  child.stdin.end()
  child.kill('SIGTERM')
  await new Promise((resolve) => child.on('exit', resolve))
}

// --- Summary ---

await cleanup()

if (process.exitCode) {
  console.log('\nFAIL')
} else {
  console.log('\nOK')
}
