#!/usr/bin/env zx

// End-to-end test for PermissionRequest hook + freud interruptions.
//
// Builds moxy and moxins from the working tree, creates a temp directory with
// settings that wire PreToolUse and PermissionRequest hooks to the local build,
// sets MOXIN_PATH so freud-interruptions can run, and drops the user into a
// shell to test manually.
//
// Usage:
//   zx zz-tests_bats/test-permission-request-hook.mjs
//
// After exiting the inner Claude Code session and the shell, the script prints:
//   1. The permission-prompts.jsonl entries written by the hook
//   2. The freud-interruptions output showing the new columns

import { spawn } from 'node:child_process'
import { homedir } from 'node:os'

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const MOXY_BIN = path.join(REPO_ROOT, 'build', 'moxy')
const MOXINS_DIR = path.join(REPO_ROOT, 'build', 'moxins')
const LIBEXEC_DIR = path.join(REPO_ROOT, 'libexec')

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

// Hook event logs — per-session JSONL files (see moxy-hooks(5))
const logHome = process.env.XDG_LOG_HOME || path.join(homedir(), '.local', 'log')
const HOOKS_LOG_DIR = path.join(logHome, 'moxy', 'hooks')

// Create temp working directory
const WORK_DIR = await fs.mkdtemp(path.join(REPO_ROOT, '.perm-test-'))

// --- Cleanup ---

let cleaningUp = false
async function cleanup() {
  if (cleaningUp) return
  cleaningUp = true
  await fs.remove(WORK_DIR).catch(() => {})
  console.log('Cleaned up temp dir.')
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

// --- Write config files ---

const hookCommand = `${MOXY_BIN} hook`

// .claude/settings.local.json — wire both PreToolUse and PermissionRequest
// hooks to the locally-built moxy binary.
await fs.ensureDir(path.join(WORK_DIR, '.claude'))
await fs.writeFile(
  path.join(WORK_DIR, '.claude', 'settings.local.json'),
  JSON.stringify(
    {
      hooks: {
        PreToolUse: [
          {
            matcher: 'mcp__moxy__.*',
            hooks: [{ type: 'command', command: hookCommand }],
          },
        ],
        PermissionRequest: [
          {
            matcher: 'mcp__moxy__.*',
            hooks: [{ type: 'command', command: hookCommand }],
          },
        ],
      },
    },
    null,
    2,
  ) + '\n',
)

// Minimal CLAUDE.md
await fs.writeFile(
  path.join(WORK_DIR, 'CLAUDE.md'),
  `# Permission Request Hook Test

Temporary test environment for PermissionRequest hook + freud interruptions.

Steps:
1. Ask Claude to use a moxy tool that requires permission (e.g. folio-external)
2. Approve or reject the prompt
3. Exit Claude Code, then exit this shell
4. Check the output for hook log entries and freud output
`,
)

// --- Print summary and drop into shell ---

console.log('')
console.log('PermissionRequest hook test environment:')
console.log(`  work dir:      ${WORK_DIR}`)
console.log(`  moxy binary:   ${MOXY_BIN}`)
console.log(`  moxins:        ${MOXINS_DIR}`)
console.log(`  hooks log dir: ${HOOKS_LOG_DIR}`)
console.log('')
console.log('Steps:')
console.log('  1. Start Claude Code:  claude')
console.log('  2. Ask it to use a moxy tool that triggers a permission prompt')
console.log('  3. Approve or reject the prompt')
console.log('  4. Exit Claude Code (/exit), then exit this shell (exit)')
console.log('')

const shell = process.env.SHELL ?? '/bin/bash'
const shellChild = spawn(shell, [], {
  stdio: 'inherit',
  cwd: WORK_DIR,
  env: {
    ...process.env,
    MOXIN_PATH: MOXINS_DIR,
  },
})

await new Promise((resolve) => {
  shellChild.on('exit', () => resolve())
  shellChild.on('error', () => resolve())
})

// --- Print results ---

console.log('')
console.log('=== 1. Hook event logs ===')
console.log(`    (${HOOKS_LOG_DIR}/)`)
console.log('')

if (await fs.pathExists(HOOKS_LOG_DIR)) {
  const logFiles = (await fs.readdir(HOOKS_LOG_DIR)).filter((f) => f.endsWith('.jsonl'))
  if (logFiles.length === 0) {
    console.log('(no session logs found)')
  } else {
    // Show the most recent log file(s)
    for (const file of logFiles.slice(-3)) {
      const sid = file.replace('.jsonl', '')
      console.log(`  session ${sid}:`)
      const raw = await fs.readFile(path.join(HOOKS_LOG_DIR, file), 'utf8')
      const lines = raw.trim().split('\n').filter((l) => l.trim())
      for (const line of lines.slice(-10)) {
        try {
          const obj = JSON.parse(line)
          console.log(`    ${obj.ts}  ${obj.hook_event_name}  ${obj.tool_name}`)
        } catch {
          console.log(`    (raw): ${line}`)
        }
      }
      console.log(`    (${lines.length} events)`)
      console.log('')
    }
  }
} else {
  console.log('(directory does not exist — no hook events were logged)')
}

console.log('')
console.log('=== 2. freud-interruptions output ===')
console.log('')

try {
  const freudResult =
    await $`MOXIN_PATH=${MOXINS_DIR} python3 ${LIBEXEC_DIR}/freud-interruptions "" 1`
  // Parse the MCP result wrapper
  try {
    const mcp = JSON.parse(freudResult.stdout)
    const text = mcp?.content?.[0]?.text ?? freudResult.stdout
    console.log(text)
  } catch {
    console.log(freudResult.stdout)
  }
} catch (e) {
  console.log(`freud-interruptions failed: ${e.message}`)
}

await cleanup()
