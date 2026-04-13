#!/usr/bin/env zx

// Launch a dev shell with the locally-built moxy binary and nix-built moxins.
//
// Usage:
//   zx bin/dev.mjs
//   just dev
//
// Builds moxy + moxins, then drops into a shell with PATH and MOXIN_PATH
// set so the local build is used instead of the nix-profile-installed one.

import { spawn } from 'node:child_process'

$.verbose = false

const SCRIPT_DIR = path.dirname(new URL(import.meta.url).pathname)
const REPO_ROOT = path.resolve(SCRIPT_DIR, '..')
const MOXY_BIN_DIR = path.join(REPO_ROOT, 'build')
const MOXINS_DIR = path.join(REPO_ROOT, 'build', 'moxins')

// --- Build ---

console.log('Building moxy and moxins...')
try {
  await $`just -f ${path.join(REPO_ROOT, 'justfile')} build-go`.pipe(process.stderr)
} catch (e) {
  console.error('Build failed:', e.message)
  process.exit(1)
}

if (!(await fs.pathExists(path.join(MOXY_BIN_DIR, 'moxy')))) {
  console.error(`error: moxy binary not found at ${MOXY_BIN_DIR}/moxy`)
  process.exit(1)
}

// --- Drop into shell ---

console.log('')
console.log('Dev shell:')
console.log(`  moxy:       ${MOXY_BIN_DIR}/moxy`)
console.log(`  MOXIN_PATH: ${MOXINS_DIR}`)
console.log('')

const shell = process.env.SHELL ?? '/bin/bash'
const shellChild = spawn(shell, [], {
  stdio: 'inherit',
  cwd: REPO_ROOT,
  env: {
    ...process.env,
    PATH: `${MOXY_BIN_DIR}:${process.env.PATH}`,
    MOXIN_PATH: MOXINS_DIR,
  },
})

const exitCode = await new Promise((resolve) => {
  shellChild.on('exit', (code) => resolve(code ?? 0))
  shellChild.on('error', () => resolve(1))
})

process.exit(exitCode)
