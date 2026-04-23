#!/usr/bin/env zx

// Bootstrap an interactive paved-paths testing environment.
//
// Creates a temporary working directory with:
//   - moxyfile.paved-paths.json (two-stage orient → edit, or caller-supplied)
//   - .mcp.json (moxy MCP server pointing at the local build)
//   - .claude/settings.json (all builtin tools denied; only this moxy enabled)
//   - logs/ (moxy stderr logs via XDG_LOG_HOME)
//
// Launches claude directly in that directory. On exit, removes the temp dir.
//
// Usage:
//   zx bin/explore-paved-paths.mjs
//   zx bin/explore-paved-paths.mjs --json path/to/paved-paths.json
//   just explore-paved-paths
//   just explore-paved-paths path/to/paved-paths.json
//
// Requires:
//   - build/moxy (run `just build-go` first)
//   - result/share/moxy/moxins (run `just build-nix` first)

import { spawn } from 'node:child_process'

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const MOXY_BIN = path.join(REPO_ROOT, 'build', 'moxy')
const MOXINS_DIR = path.join(REPO_ROOT, 'result', 'share', 'moxy', 'moxins')

// Verify binaries exist
if (!(await fs.pathExists(MOXY_BIN))) {
  console.error(`error: moxy binary not found at ${MOXY_BIN}`)
  console.error(`  run: just build-go`)
  process.exit(1)
}

if (!(await fs.pathExists(MOXINS_DIR))) {
  console.error(`error: moxins not found at ${MOXINS_DIR}`)
  console.error(`  run: just build-nix`)
  process.exit(1)
}

// Create temp working directory inside repo so .gitignore covers it
const WORK_DIR = await fs.mkdtemp(path.join(REPO_ROOT, '.paved-paths-test-'))
const LOG_DIR = path.join(WORK_DIR, 'logs')
await fs.ensureDir(LOG_DIR)

// --- Cleanup ---

let cleaningUp = false
async function cleanup() {
  if (cleaningUp) return
  cleaningUp = true
  await fs.remove(WORK_DIR).catch(() => {})
  console.log('Cleaned up.')
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

// moxyfile.paved-paths.json
const jsonSrc = argv.json
if (jsonSrc) {
  await fs.copy(jsonSrc, path.join(WORK_DIR, 'moxyfile.paved-paths.json'))
} else {
  await fs.writeJson(
    path.join(WORK_DIR, 'moxyfile.paved-paths.json'),
    [
      {
        name: 'onboarding',
        description: 'Learn the repo before making changes',
        stages: [
          { label: 'orient', tools: ['folio.read', 'folio.glob', 'rg.search'] },
          { label: 'edit',   tools: ['folio.write', 'grit.add', 'grit.commit'] },
        ],
      },
    ],
    { spaces: 2 },
  )
}

// .mcp.json — moxy MCP server with logging via XDG_LOG_HOME
await fs.writeJson(
  path.join(WORK_DIR, '.mcp.json'),
  {
    mcpServers: {
      moxy: {
        command: MOXY_BIN,
        args: ['serve', 'mcp'],
        type: 'stdio',
        env: {
          MOXIN_PATH: MOXINS_DIR,
          XDG_LOG_HOME: LOG_DIR,
        },
      },
    },
  },
  { spaces: 2 },
)

// .claude/settings.json — deny all builtin Claude tools; only load this moxy
await fs.ensureDir(path.join(WORK_DIR, '.claude'))
await fs.writeJson(
  path.join(WORK_DIR, '.claude', 'settings.json'),
  {
    enabledMcpjsonServers: ['moxy'],
    permissions: {
      deny: [
        'Bash',
        'Edit',
        'Glob',
        'Grep',
        'Read',
        'WebFetch',
        'WebSearch',
        'Write',
        'Agent',
        'NotebookEdit',
        'EnterPlanMode',
        'ExitPlanMode',
        'AskUserQuestion',
        'TaskCreate',
        'TaskUpdate',
        'TaskGet',
        'TaskList',
        'TaskOutput',
        'TaskStop',
        'CronCreate',
        'CronDelete',
        'CronList',
        'Skill',
        'EnterWorktree',
        'ExitWorktree',
        'mcp__moxy__exec-mcp',
      ],
    },
  },
  { spaces: 2 },
)

// --- Print summary and launch claude ---

console.log('')
console.log('Paved-paths test environment:')
console.log(`  work dir:  ${WORK_DIR}`)
console.log(`  moxy bin:  ${MOXY_BIN}`)
console.log(`  moxins:    ${MOXINS_DIR}`)
console.log(`  logs:      ${LOG_DIR}`)
console.log('')
console.log('Paved-paths config:')
console.log(JSON.stringify(await fs.readJson(path.join(WORK_DIR, 'moxyfile.paved-paths.json')), null, 2))
console.log('')

const claude = spawn('clown', [], {
  cwd: WORK_DIR,
  stdio: 'inherit',
})

const exitCode = await new Promise((resolve) => {
  claude.on('exit', (code) => resolve(code ?? 0))
  claude.on('error', (err) => {
    console.error(`error launching clown: ${err.message}`)
    resolve(1)
  })
})

await cleanup()
process.exit(exitCode)
