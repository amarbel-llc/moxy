#!/usr/bin/env zx

// Smoke-test hamster moxin tools by invoking the nix-built binaries directly
// and validating their output. Keeps each test fast and side-effect-free
// (go-get is skipped because it would mutate go.mod; go-mod tidy is skipped
// for the same reason — go mod verify exercises the same code path safely).
//
// Usage:
//   just test-hamster
//   zx bin/test-hamster.mjs
//
// Requires: nix-built moxins (just build-moxins) and the moxy devshell (so
// `go` resolves to the same toolchain hamster's wrappers expect).

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const HAMSTER_BIN = path.join(REPO_ROOT, 'result', 'share', 'moxy', 'moxins', 'hamster', 'bin')

function bin(tool) {
  return path.join(HAMSTER_BIN, tool)
}

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

function assertContains(haystack, needle, label) {
  if (!haystack.includes(needle)) {
    throw new Error(`${label}: expected to contain ${JSON.stringify(needle)}; got ${haystack.slice(0, 200)}`)
  }
}

function assertNotContains(haystack, needle, label) {
  if (haystack.includes(needle)) {
    throw new Error(`${label}: expected to NOT contain ${JSON.stringify(needle)}`)
  }
}

// --- build ---

console.log('Building moxins...')
await $`just -f ${path.join(REPO_ROOT, 'justfile')} build-moxins`.pipe(process.stderr)
console.log('')

// --- doc ---

console.log('doc:')

await test('stdlib package (fmt)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('doc')} fmt`).stdout
  assertContains(out, 'package fmt', 'fmt header')
  assertContains(out, 'func Println', 'fmt has Println')
  assertNotContains(out, 'Sub-packages', 'fmt should have no sub-packages')
})

await test('stdlib parent with sub-packages (encoding)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('doc')} encoding`).stdout
  assertContains(out, 'package encoding', 'encoding header')
  assertContains(out, 'Sub-packages', 'encoding should list sub-packages')
  assertContains(out, 'encoding/json', 'sub-packages should include encoding/json')
})

await test('symbol query suppresses sub-packages (encoding.BinaryMarshaler)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('doc')} encoding BinaryMarshaler`).stdout
  assertContains(out, 'BinaryMarshaler', 'symbol body present')
  assertNotContains(out, 'Sub-packages', 'symbol query must skip sub-package listing')
})

await test('markdown=true resolves stdlib via GOROOT (`fmt`)', async () => {
  // Bare stdlib names fail gomarkdoc directly ("invalid package at import path").
  // doc.ts should rewrite to $GOROOT/src/fmt and pass that. Note: gomarkdoc
  // infers the import path from the filesystem location, so the rendered
  // import is `import "std/fmt"` rather than `import "fmt"`.
  const out = (await $({ cwd: REPO_ROOT })`${bin('doc')} fmt "" true`).stdout
  assertContains(out, '# fmt', 'should render the stdlib package header')
  assertContains(out, 'Println', 'should include Println')
})

await test('markdown=true resolves external module sub-package via GOMODCACHE', async () => {
  // Module-qualified paths (github.com/...) fail gomarkdoc directly. doc.ts
  // should resolve via resolveMod() and hand gomarkdoc an absolute path
  // inside GOMODCACHE. Targets a sub-package because go-mcp's module root
  // has no top-level Go files (same limitation as `go doc`).
  const PKG = 'github.com/amarbel-llc/purse-first/libs/go-mcp/server'
  const out = (await $({ cwd: REPO_ROOT })`${bin('doc')} ${PKG} "" true`).stdout
  assertContains(out, `import "${PKG}"`, 'should render the resolved sub-package')
})

await test('markdown=true + symbol emits a discoverability hint to stderr', async () => {
  // gomarkdoc has no symbol filter; the symbol arg is silently dropped without
  // a note. We emit a stderr hint pointing callers at moxy#186 / pandoc.select
  // so a clean session sees the path forward without prior knowledge.
  const res = await $({ cwd: REPO_ROOT })`${bin('doc')} fmt Println true`.quiet()
  if (!res.stderr.includes('symbol="Println" is ignored')) {
    throw new Error(`expected stderr hint about Println; got: ${res.stderr.slice(0, 200)}`)
  }
  if (!res.stderr.includes('moxy#186')) {
    throw new Error(`expected stderr hint to reference moxy#186; got: ${res.stderr.slice(0, 200)}`)
  }
})

await test('experimental markdown=true + tags surfaces tag-gated symbols (#185)', async () => {
  // Reproduce the #185 doc/src half via the gomarkdoc backend. The default
  // `go doc` backend can't show //go:build-tagged types (golang/go#76829).
  // gomarkdoc uses go/packages which honors tags via --tags.
  const tmp = fs.mkdtempSync('/tmp/hamster-md-')
  try {
    fs.writeFileSync(path.join(tmp, 'go.mod'), 'module hamstermd\n\ngo 1.22\n')
    fs.writeFileSync(path.join(tmp, 'a.go'), 'package m\n\n// Regular is the default-tag type.\ntype Regular struct{}\n')
    fs.writeFileSync(
      path.join(tmp, 't.go'),
      '//go:build special\n\npackage m\n\n// Tagged is behind //go:build special.\ntype Tagged struct{}\n',
    )

    // markdown=true tags=special → both types render.
    const out = (await $({ cwd: tmp })`${bin('doc')} . "" true special`).stdout
    assertContains(out, 'Regular', 'gomarkdoc should render the default-tag type')
    assertContains(out, 'Tagged', 'gomarkdoc with --tags=special should render the tagged type')
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true })
  }
})

console.log('')

// --- src ---

console.log('src:')

await test('stdlib symbol (fmt.Println)', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('src')} fmt Println`).stdout
  assertContains(out, 'func Println', 'src should print the function definition')
})

console.log('')

// --- mod-read ---

console.log('mod-read:')

await test('purse-first go.mod', async () => {
  const out = (await $({ cwd: REPO_ROOT })`${bin('mod-read')} github.com/amarbel-llc/purse-first go.mod`).stdout
  assertContains(out, 'module github.com/amarbel-llc/purse-first', 'mod-read should fetch go.mod from cached module')
})

console.log('')

// --- go-vet ---

console.log('go-vet:')

await test('./internal/native/...', async () => {
  await $({ cwd: REPO_ROOT })`${bin('go-vet')} ./internal/native/...`
})

await test('cwd flag points go-vet at a subdirectory module (#174)', async () => {
  // Reproduce the #174 scenario: go.mod lives in a subdirectory, not at CWD.
  // Without `cwd`, `go vet ./...` reports
  // "directory prefix . does not contain main module".
  const tmp = fs.mkdtempSync('/tmp/hamster-cwd-')
  try {
    const sub = path.join(tmp, 'go')
    fs.mkdirSync(sub)
    fs.writeFileSync(path.join(sub, 'go.mod'), 'module hamstertest\n\ngo 1.22\n')
    fs.writeFileSync(path.join(sub, 'main.go'), 'package main\n\nfunc main() {}\n')

    // Baseline: without cwd, vet from tmp root fails.
    let baselineFailed = false
    try {
      await $({ cwd: tmp })`${bin('go-vet')} ./...`.quiet()
    } catch {
      baselineFailed = true
    }
    if (!baselineFailed) {
      throw new Error('expected baseline `go vet` to fail without cwd')
    }

    // With cwd=go, vet succeeds.
    await $({ cwd: tmp })`${bin('go-vet')} ./... "" go`
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true })
  }
})

console.log('')

// --- go-test ---

console.log('go-test:')

await test('tags flag includes test files behind //go:build (#185)', async () => {
  // Tagged file holds a Test that fails on purpose. Without tags=special the
  // file is excluded ("no test files", exit 0). With tags=special the file
  // compiles, the test runs, and the t.Fatal payload reaches stdout.
  const tmp = fs.mkdtempSync('/tmp/hamster-tags-')
  try {
    fs.writeFileSync(path.join(tmp, 'go.mod'), 'module hamstertagtest\n\ngo 1.22\n')
    fs.writeFileSync(path.join(tmp, 'lib.go'), 'package m\n')
    fs.writeFileSync(
      path.join(tmp, 'tagged_test.go'),
      '//go:build special\n\n' +
        'package m\n\n' +
        'import "testing"\n\n' +
        'func TestTagged(t *testing.T) { t.Fatal("HAMSTER_TAGGED_RAN") }\n',
    )

    // Baseline: no tags → tagged file excluded → exit 0.
    await $({ cwd: tmp })`${bin('go-test')} ./...`.quiet()

    // With tags=special: 12 positional args (everything before tags as defaults).
    let sawMarker = false
    try {
      await $({ cwd: tmp })`${bin('go-test')} ./... "" false "" false false "" "" false false "" special`.quiet()
    } catch (e) {
      if ((e.stdout || '').includes('HAMSTER_TAGGED_RAN')) sawMarker = true
    }
    if (!sawMarker) {
      throw new Error('expected tagged test to compile and run with tags=special')
    }
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true })
  }
})

console.log('')

// --- go-build ---

console.log('go-build:')

await test('./cmd/moxy → /tmp/moxy-test-build', async () => {
  const outPath = path.join('/tmp', `moxy-hamster-test-${process.pid}`)
  try {
    await $({ cwd: REPO_ROOT })`${bin('go-build')} ./cmd/moxy ${outPath} "" "" false`
    if (!fs.existsSync(outPath)) throw new Error(`expected ${outPath} to exist`)
    const stat = fs.statSync(outPath)
    if (stat.size < 1_000_000) throw new Error(`binary too small: ${stat.size} bytes`)
  } finally {
    try { fs.unlinkSync(outPath) } catch {}
  }
})

console.log('')

// --- go-mod ---

console.log('go-mod:')

await test('verify (read-only)', async () => {
  // go mod verify exits 0 and prints "all modules verified" when caches match.
  const out = (await $({ cwd: REPO_ROOT })`${bin('go-mod')} verify`).stdout
  assertContains(out, 'all modules verified', 'verify should pass')
})

await test('why (with args)', async () => {
  const argsJson = JSON.stringify(['github.com/amarbel-llc/purse-first/libs/go-mcp'])
  const out = (await $({ cwd: REPO_ROOT })`${bin('go-mod')} why ${argsJson}`).stdout
  assertContains(out, 'github.com/amarbel-llc/purse-first/libs/go-mcp', 'why should mention the queried module')
})

console.log('')

// --- go-get is skipped intentionally ---
// Mutating go.mod from a smoke test would create false-positive churn in
// VCS state. Exercising it requires an isolated temp module; left to the
// integration suite.

// --- summary ---

const total = passed + failed
console.log(`${passed}/${total} passed${failed > 0 ? `, ${failed} FAILED` : ''}`)
process.exit(failed > 0 ? 1 : 0)
