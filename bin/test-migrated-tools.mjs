#!/usr/bin/env zx

// Smoke-test migrated bun+zx tools by invoking the nix-built binaries
// directly with real CLI args. Each test calls the compiled binary from
// result/share/moxy/moxins/<server>/bin/<tool> and validates output.
//
// Usage:
//   just test-migrated-tools
//   zx bin/test-migrated-tools.mjs
//
// Requires: nix-built moxins (just build-moxins), gh auth, nix store

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const MOXINS_DIR = path.join(REPO_ROOT, 'result', 'share', 'moxy', 'moxins')

// --- PATH setup ---
// Bun-compiled binaries are symlinks that skip wrapProgram, so they
// inherit the caller's PATH. Extract the deps PATH from a wrapped bash
// sibling in each moxin and merge them so compiled binaries can find
// git, gh, find, just, etc.

function extractWrappedPath(moxinBinDir) {
  const files = fs.readdirSync(moxinBinDir)
  for (const f of files) {
    const fp = path.join(moxinBinDir, f)
    if (fs.lstatSync(fp).isSymbolicLink()) continue // skip bun binaries
    if (f.startsWith('.')) continue
    const content = fs.readFileSync(fp, 'utf8')
    const match = content.match(/export PATH='([^']+)'/)
    if (match) return match[1]
  }
  return ''
}

const moxinPaths = new Set()
for (const server of ['get-hubbed', 'get-hubbed-external', 'chix', 'just-us-agents', 'grit']) {
  const p = extractWrappedPath(path.join(MOXINS_DIR, server, 'bin'))
  if (p) for (const dir of p.split(':')) moxinPaths.add(dir)
}

// Prepend moxin deps to PATH so compiled binaries find git, gh, etc.
process.env.PATH = [...moxinPaths].join(':') + ':' + process.env.PATH

// --- helpers ---

let passed = 0
let failed = 0

async function test(name, fn) {
  try {
    await fn()
    passed++
    console.log(`  PASS  ${name}`)
  } catch (e) {
    failed++
    console.error(`  FAIL  ${name}`)
    console.error(`        ${e.message.split('\n')[0]}`)
  }
}

function bin(server, tool) {
  return path.join(MOXINS_DIR, server, 'bin', tool)
}

function assertJSON(output) {
  const parsed = JSON.parse(output)
  if (parsed === undefined) throw new Error('parsed to undefined')
  return parsed
}

function assertMCP(output) {
  const parsed = assertJSON(output)
  if (!parsed.content?.[0]?.text)
    throw new Error(`missing MCP content envelope: ${JSON.stringify(parsed).slice(0, 200)}`)
  return parsed
}

function assertArray(output) {
  const parsed = assertJSON(output)
  if (!Array.isArray(parsed))
    throw new Error(`expected array, got ${typeof parsed}`)
  return parsed
}

// --- build ---

console.log('Building moxins...')
await $`just -f ${path.join(REPO_ROOT, 'justfile')} build-moxins`.pipe(process.stderr)
console.log('')

// --- get-hubbed ---

console.log('get-hubbed:')

await test('issue-list (json)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed', 'issue-list')} open "" "" "" "" 3 json`).stdout
  const mcp = assertMCP(out)
  const issues = JSON.parse(mcp.content[0].text)
  if (!Array.isArray(issues)) throw new Error('expected array of issues')
  if (issues.length === 0) throw new Error('no issues returned')
  if (!issues[0].number) throw new Error('missing .number field')
  if (!issues[0].title) throw new Error('missing .title field')
})

await test('issue-list (text)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed', 'issue-list')} open "" "" "" "" 3 text`).stdout
  const mcp = assertMCP(out)
  if (!mcp.content[0].text.includes('#')) throw new Error('text format should contain # prefixed issue numbers')
})

await test('content-compare', async () => {
  // Use branch refs known to exist on remote (local-only commits get 404).
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed', 'content-compare')} master~1 master 5 1`).stdout
  const parsed = assertJSON(out)
  if (parsed.status === undefined) throw new Error('missing .status field')
  if (!Array.isArray(parsed.commits)) throw new Error('missing .commits array')
  if (!Array.isArray(parsed.files)) throw new Error('missing .files array')
})

await test('content-search', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed', 'content-search')} mkBunMoxin "" "" 3 1`).stdout
  const parsed = assertJSON(out)
  if (parsed.total_count === undefined) throw new Error('missing .total_count')
  if (!Array.isArray(parsed.items)) throw new Error('missing .items array')
})

console.log('')

// --- get-hubbed-external ---

console.log('get-hubbed-external:')

await test('issue-get (json)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed-external', 'issue-get')} moxy 1 "" json`).stdout
  const mcp = assertMCP(out)
  const issue = JSON.parse(mcp.content[0].text)
  if (issue.number !== 1) throw new Error(`expected issue #1, got #${issue.number}`)
})

await test('issue-get (text)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed-external', 'issue-get')} moxy 1 "" ""`).stdout
  const mcp = assertMCP(out)
  if (!mcp.content[0].text.includes('# #1:')) throw new Error('text format should start with issue header')
})

await test('issue-get (null body)', async () => {
  // Regression: the old bash+jq version crashed on `// ""` quoting
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed-external', 'issue-get')} moxy 1 "" ""`).stdout
  assertMCP(out) // should not crash
})

await test('issue-list (json)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('get-hubbed-external', 'issue-list')} moxy open "" "" "" "" 3 json`).stdout
  const mcp = assertMCP(out)
  const issues = JSON.parse(mcp.content[0].text)
  if (!Array.isArray(issues)) throw new Error('expected array')
  if (issues.length === 0) throw new Error('no issues returned')
})

console.log('')

// --- chix ---

console.log('chix:')

await test('store-ls (short)', async () => {
  // Use the moxins store path itself — guaranteed to exist
  const out = (await $`${bin('chix', 'store-ls')} ${MOXINS_DIR}/chix/bin false`).stdout
  const entries = assertArray(out)
  if (entries.length === 0) throw new Error('no entries returned')
  if (!entries[0].name) throw new Error('missing .name field')
  if (!entries[0].type) throw new Error('missing .type field')
})

await test('store-ls (long)', async () => {
  const out = (await $`${bin('chix', 'store-ls')} ${MOXINS_DIR}/chix/bin true`).stdout
  const entries = assertArray(out)
  if (entries.length === 0) throw new Error('no entries returned')
  // Long format should include size for files
  const file = entries.find(e => e.type === 'file')
  if (file && file.size === undefined) throw new Error('long format should include size for files')
})

await test('store-ls (rejects non-store path)', async () => {
  try {
    await $`${bin('chix', 'store-ls')} /tmp false`
    throw new Error('should have failed')
  } catch (e) {
    if (e.message === 'should have failed') throw e
    // expected failure
  }
})

console.log('')

// --- just-us-agents ---

console.log('just-us-agents:')

await test('list-recipes', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('just-us-agents', 'list-recipes')}`).stdout
  const mcp = assertMCP(out)
  const text = mcp.content[0].text
  if (!text.includes('build')) throw new Error('should list build recipe')
  if (!text.includes('test')) throw new Error('should list test recipe')
})

console.log('')

// --- summary ---

const total = passed + failed
console.log(`${passed}/${total} passed${failed > 0 ? `, ${failed} FAILED` : ''}`)
process.exit(failed > 0 ? 1 : 0)
