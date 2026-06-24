export MOXIN_PATH := justfile_directory() / "result-moxins" / "share" / "moxy" / "moxins"

# The hermetic gate is `nix flake check` (via `test`): it runs conformist
# (fmt + dead-jq), go-test-race, go-vet, go-lint, and every bats lane in the
# build sandbox — so the formatting/lint/go-test/bats coverage `lint` and the
# old devshell `test-*` recipes provided is now subsumed there and not
# repeated here. `lint`, `test-go`, `test-bats`, etc. remain as fast
# devshell loops for iteration; `nix flake check` is the source of truth.
default: build test test-status-clean-env

# Fast devshell lint loop (treefmt check + golangci-lint). NOT the gate — the
# hermetic equivalents (conformist + go-lint) run inside `nix flake check`.
[group("pre-build")]
lint: lint-fmt lint-go

[group("operational")]
run-dev: build-go
  zx bin/dev.mjs

# Start moxy over streamable-HTTP on an OS-assigned ephemeral port via the
# clown-plugin-protocol handshake (man clown-plugin-protocol(7)), generate
# `.tmp/.mcp.json` pointing at it, and drop into $SHELL. Moxy is killed
# when the shell exits.
[group("operational")]
run-http: build-go
  zx bin/serve-http.mjs

# POC: launch the list-changed POC MCP server (zz-pocs/list-changed/serve.ts)
# on an ephemeral port via the clown-plugin-protocol handshake, generate
# `.tmp/.mcp.json` pointing at it, and drop into $SHELL. Use to validate
# whether Claude Code refreshes its tool registry on
# `notifications/tools/list_changed`. POC is killed on shell exit.
[group("operational")]
run-poc-list-changed:
  zx bin/serve-poc-list-changed.mjs

[group("build")]
build: build-go build-nix

[group("build")]
build-go: generate build-moxins
  go build -o build/moxy ./cmd/moxy

[group("build")]
build-moxins:
  nix build --keep-going --out-link result-moxins .#moxy-moxins

# Regenerate the tommy codec, deterministically, against the flake input
# closure (mirrors dodder's `nix develop -c go generate`): `nix develop -c`
# runs go generate with the flake's pinned tommy on PATH — the same input
# flake-input-go_mod routes the cst *library* from — rather than whatever
# ambient `tommy` (devshell vs ~/.nix-profile) happens to resolve first and
# emit stale-API code. The //go:generate directive lives in
# internal/config/schema.
#
# Delete the stale generated file first (like madder's generate-tommy): tommy's
# analyze step type-checks the whole package and aborts if a prior generated
# file references a since-renamed cst API. The schema package is structured to
# compile without it, so removing it always leaves a regenerable package.
#
# `nix fmt` last applies the flake treefmt (gofumpt): tommy emits plain gofmt
# with no blank lines between top-level funcs, which lint-fmt would reject.
[group("build")]
generate:
  #!/usr/bin/env bash
  set -euo pipefail
  rm -f internal/config/schema/schema_tommy.go
  nix develop -c go generate ./internal/config/...
  nix fmt internal/config

[group("build")]
build-gomod2nix:
  gomod2nix

[group("build")]
build-nix: build-gomod2nix
  nix build --keep-going --show-trace

# Format the whole tree via treefmt-nix (goimports→gofumpt, nixfmt, shfmt for
# shell/bats, prettier for ts/mjs, tommy for toml). Config: ./treefmt.nix.
# `nix fmt` runs the same wrapper.
[group("codemod")]
codemod-fmt-treefmt *args:
  nix fmt {{args}}

# Read-only lint+format gate: build the sandboxed conformist check derivation,
# which runs every formatter (drift check) plus moxy's [linter.*] sections
# (dead-jq over the bats suite) and exits non-zero on any finding, with no
# working-tree side effects. Write-mode counterpart: codemod-fmt-treefmt
# (`nix fmt`, conformist repair mode).
[group("pre-build")]
lint-fmt:
  #!/usr/bin/env bash
  set -euo pipefail
  system=$(nix eval --raw --impure --expr 'builtins.currentSystem')
  nix build --print-build-logs --no-link ".#checks.${system}.conformist"

# Go static analysis via golangci-lint. Config: ./.golangci.yml. Hard gate via
# the `lint` aggregate. MOXIN_PATH is cleared so package loading matches the
# test-go environment.
#
# GOLANGCI_LINT_CACHE is pinned to a checkout-local dir (under the gitignored
# .tmp/) instead of the default shared ~/.cache/golangci-lint. golangci-lint
# keys cached findings by ABSOLUTE path, so a lint run inside a spinclass
# worktree (.worktrees/<name>/…) would otherwise deposit worktree-absolute
# entries into the shared cache; once that worktree is removed, a root-repo
# lint replays the dangling entry and its generated_file_filter aborts trying
# to re-read the vanished file (moxy#294). A per-checkout cache keeps
# each worktree's entries with it — and removed with it.
[group("pre-build")]
lint-go:
  GOLANGCI_LINT_CACHE='{{ justfile_directory() }}/.tmp/golangci-lint' MOXIN_PATH="" golangci-lint run

dir_build := "build"

# The gate. `test-flake-check` (= `nix flake check`) runs the hermetic
# go-test-race / go-vet / go-lint / conformist / bats-default checks, so the
# devshell test-go/test-bats and lint recipes are NOT repeated here (they stay
# as standalone fast loops). test-bats-net_cap stays explicit: the loopback
# lane needs a sandbox capability a flake check can't grant. The runtime
# smokes (validate-mcp, status) aren't expressible as flake checks either.
[group("post-build")]
test: test-flake-check test-bats-net_cap test-validate-mcp test-status

# Run the bats integration suite inside the nix build sandbox via
# `nix build .#bats-default`. The default lane filters
# `!net_cap,!host_only` — tests that need loopback binding (net_cap)
# get covered by `test-bats-net_cap`; host_only is reserved for
# tests that need host paths and runs only via `test-bats-tag
# host_only`. See #249 for why we don't run bats through
# batman/sandcastle anymore.
[group("post-build")]
test-bats:
  nix build --keep-going .#bats-default --no-link --print-build-logs

# Run the loopback-binding lane (streamable_http.bats). Verifies that
# moxy serve-http binding to 127.0.0.1 still works inside the nix build
# sandbox.
[group("post-build")]
test-bats-net_cap:
  nix build --keep-going .#bats-net_cap --no-link --print-build-logs

# Validates the flake's structural outputs (packages.* are derivations,
# devShells eval, etc). Runs last so the nix store cache is already warm
# from prior build steps.
[group("post-build")]
test-flake-check:
  nix flake check

# Run a single tag's bats lane (e.g. test-bats-tag grit, test-bats-tag
# folio, test-bats-tag net_cap). See `nix flake show` for the full list
# — auto-discovered from `# bats file_tags=` directives in
# zz-tests_bats/*.bats.
[group("post-build")]
test-bats-tag tag:
  nix build --keep-going .#bats-{{tag}} --no-link --print-build-logs

# End-to-end: verify claude -p can see and call moxy MCP tools.
# Requires: claude CLI on PATH and authenticated.
[group("post-build")]
test-smoke-claude-p: build-nix
  #!/usr/bin/env bash
  set -euo pipefail
  moxy="{{justfile_directory()}}/result/bin/moxy"
  moxin_path="{{justfile_directory()}}/result/share/moxy/moxins"
  workdir=$(mktemp -d)
  trap 'rm -rf "$workdir"' EXIT
  echo "SMOKE_TEST_CANARY_7f3a" > "$workdir/canary.txt"
  cat >"$workdir/mcp.json" <<MCPEOF
  {
    "mcpServers": {
      "moxy": {
        "command": "$moxy",
        "args": ["serve", "mcp"],
        "env": { "MOXIN_PATH": "$moxin_path" }
      }
    }
  }
  MCPEOF
  disallowed="Read,Write,Edit,Glob,Grep,WebFetch,WebSearch,Bash,Agent"
  disallowed+=",NotebookEdit,EnterPlanMode,ExitPlanMode,AskUserQuestion"
  disallowed+=",TodoWrite,EnterWorktree,ExitWorktree"
  disallowed+=",CronCreate,CronDelete,CronList,Skill"
  disallowed+=",TaskCreate,TaskUpdate,TaskGet,TaskList,TaskOutput,TaskStop"
  cd "$workdir"
  result=$(echo "Read the file canary.txt using the folio.read MCP tool. Print its exact contents. You have NO builtin tools — only MCP tools from moxy." | \
    timeout 60s claude -p \
      --dangerously-skip-permissions \
      --mcp-config "$workdir/mcp.json" \
      --disallowedTools "$disallowed" \
      2>/dev/null) || true
  if echo "$result" | grep -q "SMOKE_TEST_CANARY_7f3a"; then
    echo "ok: claude -p read canary file via moxy MCP tool"
  else
    echo "FAIL: canary content not found in claude -p output" >&2
    echo "--- output ---" >&2
    echo "$result" >&2
    exit 1
  fi

# Smoke-test migrated bun+zx tool scripts against real APIs
[group("post-build")]
test-migrated-tools: build-moxins
  nix run nixpkgs#bun -- x zx bin/test-migrated-tools.mjs

# Smoke-test the locally-built hamster moxin (doc, src, mod-read, go-mod)
[group("post-build")]
test-hamster: build-moxins
  nix run nixpkgs#bun -- x zx bin/test-hamster.mjs

[group("post-build")]
test-go:
  MOXIN_PATH="" go vet ./...
  MOXIN_PATH="" go test ./... -v

# Per-function coverage report for a Go package.
# Used during refactors to identify untested branches before moving code.
# Example: just test-go-cover ./internal/hook/...
[group("post-build")]
test-go-cover pkg=".":
  #!/usr/bin/env bash
  set -euo pipefail
  mkdir -p .tmp
  out=.tmp/cover-$(echo "{{pkg}}" | tr '/.' '__').out
  MOXIN_PATH="" go test "{{pkg}}" -coverprofile="$out" -covermode=atomic
  echo
  echo "--- per-function coverage ---"
  go tool cover -func="$out"

# Drive a freshly-built moxy from `claude -p`, exercising the `batch` builtin
# end-to-end (MCP handshake, tool registration, dispatch, NDJSON output).
# Why a recipe: clown's mkCircus pins moxy at build time, so the moxy
# inside an interactive Claude Code session can't see new builtins. This
# recipe launches a fresh `claude -p` process configured to use the
# worktree's just-built moxy via --mcp-config, so the new builtin is
# visible. Mirrors test-smoke-claude-p's shape.
[group("post-build")]
test-batch-via-claude-p: build-nix
  #!/usr/bin/env bash
  set -euo pipefail
  moxy="{{justfile_directory()}}/result/bin/moxy"
  moxin_path="{{justfile_directory()}}/result/share/moxy/moxins"
  workdir=$(mktemp -d)
  trap 'rm -rf "$workdir"' EXIT
  cat >"$workdir/mcp.json" <<MCPEOF
  {
    "mcpServers": {
      "moxy": {
        "command": "$moxy",
        "args": ["serve", "mcp"],
        "env": { "MOXIN_PATH": "$moxin_path" }
      }
    }
  }
  MCPEOF
  disallowed="Read,Write,Edit,Glob,Grep,WebFetch,WebSearch,Bash,Agent"
  disallowed+=",NotebookEdit,EnterPlanMode,ExitPlanMode,AskUserQuestion"
  disallowed+=",TodoWrite,EnterWorktree,ExitWorktree"
  disallowed+=",CronCreate,CronDelete,CronList,Skill"
  disallowed+=",TaskCreate,TaskUpdate,TaskGet,TaskList,TaskOutput,TaskStop"
  cd "$workdir"
  prompt='Call the `batch` MCP tool with this exact JSON arguments object:
  {"calls":[{"tool":"folio.glob","args":{"pattern":"*.md"}},{"tool":"folio.glob","args":{"pattern":"*.toml"}}]}
  Print only the raw NDJSON output. You have NO builtin tools — only MCP tools from moxy.'
  echo "$prompt" | timeout 90s claude -p \
    --dangerously-skip-permissions \
    --mcp-config "$workdir/mcp.json" \
    --allowedTools "mcp__moxy__batch,mcp__moxy__folio.glob" \
    --disallowedTools "$disallowed"

[group("post-build")]
test-status: build-go
  {{justfile_directory()}}/{{dir_build}}/moxy status

# Verify the nix-built binary discovers system moxins without ambient env
[group("post-build")]
test-status-clean-env: build-nix
  #!/usr/bin/env bash
  set -euo pipefail
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  mkdir -p "$tmpdir/home/repo"
  true_bin=$(type -P true)
  printf '[[servers]]\nname = "test"\ncommand = "%s"\n' "$true_bin" \
    >"$tmpdir/home/repo/moxyfile"
  cd "$tmpdir/home/repo"
  out=$(env -i HOME="$tmpdir/home" PATH="$(dirname "$true_bin")" \
    "{{justfile_directory()}}/result/bin/moxy" status)
  echo "$out"
  echo "$out" | grep -q "moxin(s)"
  echo "$out" | grep -q "all checks passed"

[group("post-build")]
test-validate-mcp: build-go
  #!/usr/bin/env bash
  set -euo pipefail
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  export HOME="$tmpdir/home"
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
  [[servers]]
  name = "test"
  command = ["bash", "{{justfile_directory()}}/zz-tests_bats/test-fixtures/tool-server.bash"]
  EOF
  # Moxy fails startup if the .default madder store is missing; init one
  # at $HOME so walking up from $HOME/repo finds it.
  (cd "$HOME" && madder init .default >/dev/null 2>&1)
  cd "$HOME/repo"
  purse-first validate-mcp {{justfile_directory()}}/{{dir_build}}/moxy serve mcp

# One-shot codemod for #315: append [annotations] to the tool TOMLs that
# validate-mcp flags as unannotated. Idempotent (skips files that already
# have an [annotations] block). Per-tool semantics encoded inline.
[group("debug")]
debug-codemod-annotations:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}
  append() {
    local f="$1"; shift
    grep -q '^\[annotations\]' "$f" && return 0
    { printf '\n[annotations]\n'; printf '%s\n' "$@"; } >> "$f"
    echo "annotated: $f"
  }
  # Re-running creates a new commit/stash/branch or errors — never converges.
  append moxins/grit/branch-create.toml 'idempotent-hint = false'
  append moxins/grit/cherry-pick.toml   'idempotent-hint = false'
  append moxins/grit/commit.toml        'idempotent-hint = false'
  append moxins/grit/merge.toml         'idempotent-hint = false'
  append moxins/grit/revert.toml        'idempotent-hint = false'
  append moxins/grit/stash-save.toml    'idempotent-hint = false'
  # History rewriters.
  append moxins/grit/rebase.toml  'destructive-hint = true' 'idempotent-hint = false'
  append moxins/grit/restack.toml 'destructive-hint = true' 'idempotent-hint = false'
  # Soft reset: abandons branch-tip refs but converges on the same target
  # (matches hard-reset's destructive+idempotent pair).
  append moxins/grit/reset.toml 'destructive-hint = true' 'idempotent-hint = true'
  # Template instantiation: refuses/conflicts on existing files.
  append moxins/chix/flake-init.toml 'idempotent-hint = false'

# Sleep for N seconds (default 300). Deterministic long-running target for
# the async-cancel live smoke (FDR 0004) — every cached/test job finishes
# faster than an agent's inter-turn latency, making cancellation unprovable
# against real workloads.
[group("debug")]
debug-sleep seconds="300":
  sleep {{seconds}}

# Run a focused Go test by name pattern in one package (default ./internal/...).
# Fast inner loop for TDD on a single test without the full `test-go` suite.
# MOXIN_PATH is cleared to match test-go's package-loading environment.
[group("debug")]
debug-go-test pattern pkg="./internal/native/...":
  MOXIN_PATH="" go test {{pkg}} -run '{{pattern}}' -v

# One-shot codemod for #318: insert `permit-async = false` after the
# perms-request line in ordering-sensitive / trivially-fast moxin tools.
# Idempotent (skips files that already declare permit-async). Keep for
# reference; safe to re-run.
[group("debug")]
debug-codemod-permit-async:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}
  files=(
    moxins/grit/{add,branch-create,checkout,cherry-pick,commit,diff,git-rev-parse,log,merge,mv,pull,rebase,reset,restack,revert,rm,stash-apply,stash-save,status,tag,worktree-list}.toml
    moxins/folio/{chmod,cp,file-type,link,ls,mkdir,mktemp,mv,read,rm,tar,write}.toml
    moxins/env/{get,resolve,which}.toml
    moxins/jq/jq.toml
    moxins/arboretum/{md-toc,md-section,md-anchor}.toml
    moxins/man/{list,toc,section}.toml
    moxins/just-us-agents/{list-recipes,show-recipe,list-variables,dump-justfile}.toml
    moxins/hamster/{mod-read,src}.toml
    moxins/chix/{which,nix-hash,store-ls,store-cat,store-path-info,derivation-show,flake-init}.toml
  )
  changed=0
  for f in "${files[@]}"; do
    [ -f "$f" ] || { echo "MISSING: $f" >&2; exit 1; }
    grep -q '^permit-async' "$f" && continue
    grep -q '^perms-request = ' "$f" || { echo "NO perms-request: $f" >&2; exit 1; }
    sed -i '/^perms-request = /a permit-async = false' "$f"
    changed=$((changed + 1))
  done
  echo "annotated: $changed"
  echo "total with permit-async = false: $(grep -rl '^permit-async = false' moxins --include='*.toml' | wc -l)"

# Probe whether `madder init <unprefixed-id>` creates an XDG user-level store
# under a fresh XDG_DATA_HOME. Agent dev-loop for FDR 0004's async result
# store (moxy-async).
[group("debug")]
debug-madder-init-xdg:
  #!/usr/bin/env bash
  set -euo pipefail
  # mktemp -p /tmp escapes the worktree's .madder ancestry (TMPDIR points
  # into the worktree .tmp, whose walk-up shadows XDG scope — madder#227).
  tmp=$(mktemp -d -p /tmp madder-xdg-probe.XXXXXX)
  trap 'rm -rf "$tmp"' EXIT
  mkdir -p "$tmp/cwd" "$tmp/xdg"
  cd "$tmp/cwd"
  XDG_DATA_HOME="$tmp/xdg" madder init moxy-async
  echo "--- created under xdg: ---"
  find "$tmp/xdg" -maxdepth 5 | head -20
  echo "--- write round trip: ---"
  printf 'roundtrip-ok' | XDG_DATA_HOME="$tmp/xdg" madder write -format json moxy-async - | head -1

# Dump one tool's tools/list entry from the locally-built moxy under a given
# MCP protocolVersion. Agent dev-loop for #217 (annotations reported missing).
[group("debug")]
debug-dump-tool-entry tool="folio.read" protocol="2025-11-25": build-go
  #!/usr/bin/env bash
  set -euo pipefail
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  export HOME="$tmpdir/home"
  mkdir -p "$HOME"
  (cd "$HOME" && madder init .default >/dev/null 2>&1)
  cd "$HOME"
  init=$(jq -cn --arg v '{{protocol}}' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":$v,"capabilities":{},"clientInfo":{"name":"debug","version":"0"}}}')
  initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  list='{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
  # The sleep after initialize matters: the purse-first server handles
  # messages in goroutines, so a pipelined tools/list can race ahead of
  # initialize's version negotiation and get the V0 (annotation-less) listing.
  (echo "$init"; sleep 0.5; echo "$initialized"; echo "$list"; sleep 2) \
    | {{justfile_directory()}}/{{dir_build}}/moxy serve mcp 2>/dev/null \
    | jq -c 'if .id == 1 then {negotiated: .result.protocolVersion} elif .id == 2 then (.result.tools[] | select(.name == "{{tool}}")) else empty end'

# Bisect helper: build and validate MCP loading at current commit
# Usage: git bisect start HEAD <known-good> -- && git bisect run just debug-bisect
[group("debug")]
debug-bisect: build-go
  #!/usr/bin/env bash
  set -euo pipefail
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  export HOME="$tmpdir/home"
  mkdir -p "$HOME/repo"
  export MOXIN_PATH="{{justfile_directory()}}/result-moxins/share/moxy/moxins"
  cat >"$HOME/repo/moxyfile" <<EOF
  [[servers]]
  name = "test"
  command = ["bash", "{{justfile_directory()}}/zz-tests_bats/test-fixtures/tool-server.bash"]
  EOF
  cd "$HOME/repo"
  moxy="{{justfile_directory()}}/{{dir_build}}/moxy"

  # Phase 1: MCP protocol compliance
  purse-first validate-mcp "$moxy" serve mcp

  # Phase 2: verify moxin tools are actually discovered
  init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"bisect","version":"0.1"}}}'
  notif='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  list='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  tools=$(timeout --preserve-status 10s bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | "$0" serve mcp 2>/dev/null | jq -c "select(.id == 2) | .result.tools"' \
    "$moxy" "$init" "$notif" "$list")
  count=$(echo "$tools" | jq 'length')
  # With moxins loaded we expect 100+ tools; without them only ~5 (test server + meta tools)
  if (( count < 20 )); then
    echo "FAIL: only $count tools found (expected 100+), moxins likely failed to load" >&2
    exit 1
  fi
  echo "ok: $count tools discovered (moxins loaded)"

mcp-inspect := "npx @modelcontextprotocol/inspector --cli"

[group("post-build")]
test-mcp: build-go
  #!/usr/bin/env nix
  #! nix shell nixpkgs#nodejs --command bash
  set -euo pipefail
  tools=$({{mcp-inspect}} --method tools/list {{justfile_directory()}}/{{dir_build}}/moxy serve mcp)
  echo "$tools" | jq .

[group("operational")]
run-nix *ARGS:
  nix run . -- {{ARGS}}

[group("maintenance")]
update: update-go

[group("maintenance")]
update-go:
  env GOPROXY=direct go get -u -t ./...
  go mod tidy

[group("inspection")]
man-list section="1":
  apropos -s {{section}} . 2>/dev/null | sort -u

[group("inspection")]
man-count section="1":
  apropos -s {{section}} . 2>/dev/null | sort -u | wc -l

[group("inspection")]
man-count-all:
  @for s in 1 2 3 4 5 6 7 8; do \
    count=$(apropos -s $s . 2>/dev/null | sort -u | wc -l | tr -d ' '); \
    printf "section %s: %s pages\n" "$s" "$count"; \
  done

[group("inspection")]
man-search query section="1":
  apropos -s {{section}} {{query}} 2>/dev/null | sort -u

# Bump MOXY_VERSION in version.env to the given semver
[group("maintenance")]
bump-version new_version:
  #!/usr/bin/env bash
  set -euo pipefail
  . version.env
  current="$MOXY_VERSION"
  if [[ "$current" == "{{new_version}}" ]]; then
    echo "already at {{new_version}}" >&2
    exit 0
  fi
  sed -i.bak 's/^export MOXY_VERSION=.*/export MOXY_VERSION={{new_version}}/' version.env && rm version.env.bak
  echo "$current → {{new_version}}"

# Create a signed git tag for the current MOXY_VERSION, verify the signature, then push to origin. Message defaults to "Release v<ver>"; pass a multi-line changelog as the arg for richer notes.
# [positional-arguments] passes `message` as $1 (a real argv element) instead of
# textually interpolating {{message}} into the bash body — a changelog whose
# commit subjects contain shell metacharacters (e.g. a literal `$output`) would
# otherwise be expanded by bash and, under `set -u`, abort with "unbound variable".
[group("maintenance")]
[positional-arguments]
tag message="":
  #!/usr/bin/env bash
  set -euo pipefail
  . version.env
  tag="v${MOXY_VERSION}"
  if git rev-parse "$tag" >/dev/null 2>&1; then
    echo "tag $tag already exists" >&2
    exit 1
  fi
  message="${1:-}"
  if [[ -z "$message" ]]; then
    message="Release $tag"
  fi
  git tag -s "$tag" -m "$message"
  git tag -v "$tag"
  echo "created and verified tag $tag"
  git push origin "$tag"
  echo "pushed tag $tag"

# Generate auto-changelog (last tag → HEAD, before bumping so the
# release commit doesn't appear in its own notes), bump MOXY_VERSION
# on master, commit, push master, create signed tag with the
# changelog as the tag message, and publish the GitHub release with
# the same changelog as the release body. Pass notes_file to
# override the auto-changelog with hand-written notes. Must be run
# from the master branch.
# [positional-arguments] exposes new_version as $1 and notes_file as $2 as real
# argv elements rather than {{...}} text-substituting them into the bash body.
[group("maintenance")]
[positional-arguments]
release new_version notes_file="":
  #!/usr/bin/env bash
  set -euo pipefail
  new_version="$1"
  notes_file="${2:-}"
  current_branch=$(git rev-parse --abbrev-ref HEAD)
  if [[ "$current_branch" != "master" ]]; then
    echo "just release must be run on master (currently on $current_branch)" >&2
    exit 1
  fi
  if [[ -n "$notes_file" ]]; then
    notes=$(cat "$notes_file")
  else
    last_tag=$(git describe --tags --abbrev=0 2>/dev/null || true)
    if [[ -n "$last_tag" ]]; then
      notes=$(git log --format='- %s' "${last_tag}..HEAD")
    else
      notes=$(git log --format='- %s' HEAD)
    fi
  fi
  just bump-version "$new_version"
  if ! git diff --quiet version.env; then
    git add version.env
    git commit -m "chore: release v${new_version}"
  fi
  git push origin master
  just tag "$notes"
  gh release create "v${new_version}" --title "v${new_version}" --notes "$notes"

[group("maintenance")]
clean: clean-build

[group("maintenance")]
clean-build:
  rm -rf result build/

# Run `bun install` at the repo root (refresh bun.lock for mkBunMoxin bundling)
[group("debug")]
debug-bun-install:
  cd {{justfile_directory()}} && bun install

# Regenerate bun.nix from bun.lock via nix-community/bun2nix
[group("debug")]
debug-bun2nix:
  cd {{justfile_directory()}} && nix run github:nix-community/bun2nix -- -o bun.nix

# Smoke-test arboretum-moxin outline against POC sample
[group("debug")]
debug-arboretum-smoke:
  {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/outline {{justfile_directory()}}/zz-pocs/outline-poc/samples/sample.go

# Fast local dead-jq check (the conformist [linter.dead-jq] command, run
# directly without the nix build wrapper). The gating path is `just lint-fmt`
# (conformist check); this recipe is a quicker loop while editing bats files.
[group("debug")]
debug-lint-dead-jq *files:
  bash {{justfile_directory()}}/scripts/lint-dead-jq {{files}}

# Smoke-test arboretum-moxin search against a small fixture
[group("debug")]
debug-arboretum-search-smoke:
  {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/search 'console.log($MSG)' {{justfile_directory()}}/.tmp/astgrep-smoke

# Smoke-test arboretum-moxin search against a small Go fixture (lang=go)
[group("debug")]
debug-arboretum-search-go-smoke:
  {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/search 'fmt.Println($X)' {{justfile_directory()}}/.tmp/astgrep-smoke go

# Smoke-test arboretum-moxin rewrite (apply) against a small Go fixture
[group("debug")]
debug-arboretum-rewrite-go-smoke:
  {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/rewrite 'fmt.Println($X)' 'log.Info($X)' {{justfile_directory()}}/.tmp/astgrep-smoke go '' false

# Smoke-test arboretum md-toc against a tiny markdown blob on stdin
[group("debug")]
debug-arboretum-md-toc-smoke:
  printf '# Hello\n\n## World\n\nbody\n\n## Again\n' | {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/md-toc

# Smoke-test arboretum md-section against a tiny markdown blob on stdin
[group("debug")]
debug-arboretum-md-section-smoke:
  printf '# Hello\n\n## World\n\nbody\n\n## Again\nmore\n' | {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/md-section World

# Smoke-test arboretum md-anchor against a tiny markdown blob on stdin
[group("debug")]
debug-arboretum-md-anchor-smoke:
  printf '<a name="x"></a>\n# X\nbody\n\n<a name="y"></a>\n# Y\nmore\n' | {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/md-anchor x

# Smoke-test arboretum-moxin rewrite (dry-run) against a small fixture
[group("debug")]
debug-arboretum-rewrite-smoke:
  {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/rewrite 'console.log($MSG)' 'logger.info($MSG)' {{justfile_directory()}}/.tmp/astgrep-smoke '' '' true

# Smoke-test arboretum-moxin rewrite (apply) against a small fixture
[group("debug")]
debug-arboretum-rewrite-apply-smoke:
  {{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/rewrite 'console.log($MSG)' 'logger.info($MSG)' {{justfile_directory()}}/.tmp/astgrep-smoke '' '' false

# Probe ast-grep's --update-all output streams independently
[group("debug")]
debug-astgrep-streams:
  #!/usr/bin/env bash
  set -uo pipefail
  cd {{justfile_directory()}}
  cat > .tmp/astgrep-smoke/test.js <<'EOF'
  console.log("startup");
  console.log("done");
  EOF
  ag=/nix/store/zfpg4kzi0lw9a18nld7q212pjp1galkl-ast-grep-0.42.1/bin/ast-grep
  echo "=== STDOUT ONLY ==="
  "$ag" run -p 'console.log($MSG)' -r 'logger.info($MSG)' --update-all .tmp/astgrep-smoke 2>/dev/null
  cat > .tmp/astgrep-smoke/test.js <<'EOF'
  console.log("startup");
  console.log("done");
  EOF
  echo "=== STDERR ONLY ==="
  "$ag" run -p 'console.log($MSG)' -r 'logger.info($MSG)' --update-all .tmp/astgrep-smoke 1>/dev/null

# Re-capture arboretum golden-output fixtures from the nix-built binary
[group("debug")]
debug-arboretum-regen-goldens:
  #!/usr/bin/env bash
  set -euo pipefail
  bin={{justfile_directory()}}/result-moxins/share/moxy/moxins/arboretum/bin/outline
  fixtures={{justfile_directory()}}/zz-tests_bats/test-fixtures/arboretum
  for f in "$fixtures"/sample.*; do
    case "$f" in *.golden) continue;; esac
    name=$(basename "$f")
    "$bin" "$f" | sed "s|$f|samples/$name|" > "$f.golden"
    echo "wrote $f.golden"
  done

# Integration test for moxin discovery via a fresh temp workspace
[group("post-build")]
test-moxin-loading:
  zx bin/test-moxin-loading.mjs

# Integration test for internal/stderrlog per-session logging + rotation flow.
[group("post-build")]
test-stderrlog:
  zx bin/test-stderrlog.mjs

# Debug: look for OOM kills in kernel ring buffer (needs pkexec; user sasha not in adm/systemd-journal groups)
# SHELL is sanitized to /bin/bash since pkexec rejects SHELL values not in /etc/shells.
[group("debug")]
debug-pkexec-oom days='8':
  #!/usr/bin/env bash
  set +e
  export SHELL=/bin/bash
  echo "=== pkexec dmesg -T | grep OOM/killed ==="
  pkexec dmesg -T 2>&1 | tee /tmp/_dmesg.out | grep -iE 'killed process|out of memory|oom-kill|invoked oom-killer' | tail -50
  echo "(dmesg total lines: $(wc -l < /tmp/_dmesg.out), sample last line: $(tail -1 /tmp/_dmesg.out))"
  echo ""
  echo "=== pkexec journalctl -k --since -{{days}}d (OOM-related) ==="
  pkexec journalctl -k --since "{{days}} days ago" --no-pager 2>&1 | tee /tmp/_jk.out | grep -iE 'killed process|out of memory|oom-kill|invoked oom-killer' | tail -50
  echo "(kernel journal lines: $(wc -l < /tmp/_jk.out))"
  echo ""
  echo "=== pkexec journalctl --since -{{days}}d systemd-oomd ==="
  pkexec journalctl --since "{{days}} days ago" --no-pager -u systemd-oomd.service 2>&1 | tail -50
  rm -f /tmp/_dmesg.out /tmp/_jk.out

[group("explore")]
explore-nix-tools-list: build-nix
  #!/usr/bin/env bash
  set -euo pipefail
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  export HOME="$tmpdir/home"
  mkdir -p "$HOME/repo"
  export MOXIN_PATH="{{justfile_directory()}}/result/share/moxy/moxins"
  cd "$HOME/repo"
  moxy="{{justfile_directory()}}/result/bin/moxy"

  init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"explore","version":"0.1"}}}'
  notif='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  list='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

  echo "--- initialize response ---"
  init_result=$(timeout --preserve-status 10s bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | "$0" serve mcp 2>/tmp/moxy-stderr.log | jq -c "select(.id)" | head -2' \
    "$moxy" "$init" "$notif" "$list")
  echo "$init_result" | jq .
  echo ""
  echo "--- stderr ---"
  cat /tmp/moxy-stderr.log || true
  echo ""

  count=$(echo "$init_result" | tail -1 | jq '.result.tools | length' 2>/dev/null || echo "PARSE_ERROR")
  echo "Tool count: $count"

# Test validate-mcp against serve-moxin with devshell-built binary.
# Usage: just debug-validate-serve-moxin piers
[group("debug")]
debug-validate-serve-moxin name: build-go
  #!/usr/bin/env bash
  set -euo pipefail
  moxy="{{justfile_directory()}}/{{dir_build}}/moxy"

  echo "=== Manual MCP handshake ==="
  init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  notif='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  list='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
  result=$(timeout --preserve-status 10s bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | "$0" serve-moxin --name "$4" 2>/tmp/serve-moxin-stderr.log' \
    "$moxy" "$init" "$notif" "$list" "{{name}}" || true)
  echo "stdout:"
  echo "$result" | head -20
  echo ""
  echo "stderr:"
  cat /tmp/serve-moxin-stderr.log | head -20
  echo ""

  echo "=== purse-first validate-mcp (verbose) ==="
  purse-first validate-mcp "$moxy" serve-moxin --name "{{name}}" 2>&1 \
    && echo "PASS" || echo "FAIL (exit $?)"

# Run sisyphus Python unit tests (lib/test_*.py) using the nix-wrapped
# python3 from the sisyphus moxin (which has marklas + mistune available).
# Agent dev-loop: run after editing _validate.py / _issuetype.py or their tests.
[group("debug")]
debug-sisyphus-py-tests: build-moxins
  #!/usr/bin/env bash
  set -euo pipefail
  moxin_dir="{{justfile_directory()}}/result-moxins/share/moxy/moxins/sisyphus"
  # The nix wrapper burns in the python path; extract it from the create-issue wrapper.
  py_bin=$(grep -o '/nix/store/[^:]*-python3[^/]*/bin' "$moxin_dir/bin/create-issue" | head -1)/python3
  rc=0
  for t in "{{justfile_directory()}}"/moxins/sisyphus/lib/test_*.py; do
    echo "== $(basename "$t") =="
    "$py_bin" "$t" || rc=1
  done
  exit "$rc"

# Lenient mypy type-check (#10) of the first-party moxin Python via the same
# wrapped checker the gate's [linter.mypy] runs (mypy + types-requests, reading
# ./mypy.ini). The checker skips the one bash script (api-perms) by shebang.
# Agent dev-loop: run after editing moxin Python or mypy.ini.
[group("debug")]
debug-py-typecheck:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}
  nix build --keep-going .#lint-py-types -o result-lint-py
  ./result-lint-py/bin/lint-py-types \
    moxins/sisyphus/lib/*.py \
    moxins/sisyphus/bin/* \
    moxins/freud/bin/*

# Probe what marklas produces for the #239 pipe-prose and diff-codeblock cases.
# Agent dev-loop: run to inspect ADF output before writing validator/tests.
[group("debug")]
debug-sisyphus-239-probe: build-moxins
  #!/usr/bin/env bash
  set -euo pipefail
  root="{{justfile_directory()}}"
  moxin_dir="$root/result-moxins/share/moxy/moxins/sisyphus"
  py_bin=$(grep -o '/nix/store/[^:]*-python3[^/]*/bin' "$moxin_dir/bin/create-issue" | head -1)/python3
  VENDOR="$root/moxins/sisyphus/lib/_vendor" \
    "$py_bin" "$root/moxins/sisyphus/lib/probe_239.py"

# Reproduce tools-not-appearing via claude -p with the nix-built moxy.
[group("explore")]
explore-claude-p: build-nix
  bin/explore-claude-p.bash "{{justfile_directory()}}"

# Build the dynamic-perms POC driver. POC scope only — not wired into main test.
[group("explore")]
poc-build-dynamic-perms:
  go build -o build/moxy-exporel-dynamic-perms ./cmd/moxy-exporel-dynamic-perms

# Run the dynamic-perms POC bats wrapper. Driver self-asserts.
[group("explore")]
poc-test-dynamic-perms: poc-build-dynamic-perms
  bats {{justfile_directory()}}/zz-bats_explore/dynamic_perms_poc.bats

# Enable impure-derivations + ca-derivations on the nix-daemon and restart it.
# Idempotent: re-running is a no-op if both features are already in nix.custom.conf.
# Mirrors amarbel-llc/eng#41's resolution but for Determinate Nix on Linux instead of darwin.
[group("debug")]
debug-pkexec-enable-impure-derivations:
  #!/usr/bin/env bash
  set -euo pipefail
  export SHELL=/bin/bash
  echo "=== before ==="
  grep -E '^(extra-)?experimental-features' /etc/nix/nix.custom.conf || true
  echo ""
  echo "=== pkexec edit + daemon restart ==="
  pkexec bash -c '
    set -euo pipefail
    conf=/etc/nix/nix.custom.conf
    if grep -qE "^extra-experimental-features.*\bimpure-derivations\b" "$conf"; then
      echo "[skip] extra-experimental-features already enables impure-derivations"
    else
      printf "\n# Added for moxy chix.bash prototype (mirrors amarbel-llc/eng#41).\n" >> "$conf"
      printf "extra-experimental-features = impure-derivations ca-derivations\n" >> "$conf"
      echo "[appended] extra-experimental-features = impure-derivations ca-derivations"
    fi
    systemctl restart nix-daemon.service
    systemctl is-active nix-daemon.service
  '
  echo ""
  echo "=== after ==="
  grep -E '^(extra-)?experimental-features|^# Added for moxy' /etc/nix/nix.custom.conf || true
  echo ""
  echo "=== nix config show (post-restart) ==="
  nix config show 2>/dev/null | grep -i experimental || nix-instantiate --eval --expr 'builtins.currentSystem' 2>&1 | tail -3

# Look up nix.conf docs for a setting via `nix config show --json` to get its description.
[group("debug")]
debug-nix-setting key:
  #!/usr/bin/env bash
  set -euo pipefail
  nix config show --json 2>/dev/null | jq --arg k '{{key}}' '.[$k] // .[($k | sub("^extra-"; ""))]'

# Smallest possible __impure derivation, exercise every flag override path.
[group("debug")]
debug-nix-impure-min:
  #!/usr/bin/env bash
  set -uo pipefail
  drv=$(mktemp --suffix=.nix)
  trap 'rm -f "$drv"' EXIT
  cat > "$drv" <<'NIX'
  { pkgs ? import <nixpkgs> {} }:
  pkgs.runCommand "impure-min" {
    __impure = true;
    buildInputs = [ pkgs.coreutils ];
  } ''
    echo hello > $out
  ''
  NIX

  echo "=== A: --extra-experimental-features (subcommand flag) ==="
  nix build --impure --no-link --print-out-paths \
    --extra-experimental-features impure-derivations \
    --extra-experimental-features ca-derivations \
    --file "$drv" 2>&1 | tail -5
  echo ""
  echo "=== B: --option experimental-features ==="
  nix build --impure --no-link --print-out-paths \
    --option experimental-features 'nix-command flakes impure-derivations ca-derivations' \
    --file "$drv" 2>&1 | tail -5
  echo ""
  echo "=== C: NIX_CONFIG env ==="
  NIX_CONFIG="experimental-features = nix-command flakes impure-derivations ca-derivations" \
    nix build --impure --no-link --print-out-paths --file "$drv" 2>&1 | tail -5
  echo ""
  echo "=== D: pre-subcommand --extra-experimental-features ==="
  nix --extra-experimental-features impure-derivations \
      --extra-experimental-features ca-derivations \
      build --impure --no-link --print-out-paths --file "$drv" 2>&1 | tail -5

# Probe nix capabilities (version, experimental features) for chix.bash work.
[group("debug")]
debug-nix-features:
  #!/usr/bin/env bash
  set -euo pipefail
  echo "--- nix --version ---"
  nix --version
  echo ""
  echo "--- nix --help | grep experimental ---"
  nix --help 2>&1 | grep -i experimental || echo "(no match)"
  echo ""
  echo "--- nix-build --help | grep -E 'experimental|option' ---"
  nix-build --help 2>&1 | grep -iE 'experimental|option' || echo "(no match)"
  echo ""
  echo "--- try --option experimental-features ---"
  echo 'derivation { name = "x"; system = "x86_64-linux"; builder = "/bin/sh"; }' | \
    nix-instantiate --option experimental-features "nix-command flakes impure-derivations ca-derivations" \
    --expr 'builtins.toString (derivation { name = "x"; system = "x86_64-linux"; builder = "/bin/sh"; __impure = true; })' \
    2>&1 | head -20 || true

# Probe: does --option extra-sandbox-paths take effect from a non-trusted user?
# Tries to bind-mount /home/sasha/eng/repos/moxy/.worktrees/snug-sumac into the sandbox
# and `ls` inside. If the bind silently fails, the worktree path won't exist in the sandbox.
[group("debug")]
debug-extra-sandbox-paths:
  #!/usr/bin/env bash
  set -uo pipefail
  drv=$(mktemp --suffix=.nix)
  trap 'rm -f "$drv"' EXIT
  cwd="{{justfile_directory()}}"
  cat > "$drv" <<NIX
  { pkgs ? import <nixpkgs> {} }:
  pkgs.runCommand "extra-sandbox-paths-probe" {
    __impure = true;
    buildInputs = [ pkgs.coreutils ];
  } ''
    mkdir -p \$out
    set +e
    echo "ls $cwd:" > \$out/result
    ls -la "$cwd" >> \$out/result 2>&1
    echo "" >> \$out/result
    echo "ls $cwd/flake.nix:" >> \$out/result
    ls -la "$cwd/flake.nix" >> \$out/result 2>&1
    set -e
  ''
  NIX
  echo "--- with --option extra-sandbox-paths ---"
  out=$(nix build --impure --no-link --print-out-paths \
    --option extra-sandbox-paths "$cwd" \
    --file "$drv" 2>&1) || { echo "BUILD FAILED:"; echo "$out"; exit 1; }
  cat "$out/result"

# Reproduce the folio-perms sibling-repo read decision for a spinclass-style
# linked worktree (debugging the git-root resolution in bin/folio-perms).
# Builds a main repo + commit + linked worktree under a temp dir, then runs
# the SOURCE folio-perms (with the devshell's git on PATH) under `bash -x`
# so the git resolution, siblings_root, and main_worktree values are visible.
[group("debug")]
debug-folio-perms-linked-worktree:
  #!/usr/bin/env bash
  set -uo pipefail
  cd {{justfile_directory()}}
  # Must live OUTSIDE the moxy worktree, else the sibling path falls under
  # CWD and is allowed by the CWD rule instead of the siblings rule.
  root=$(mktemp -d -p /tmp folio-dbg.XXXXXX)
  trap 'rm -rf "$root"' EXIT
  mkdir -p "$root/repos/myrepo" "$root/repos/sibling"
  git -C "$root/repos/myrepo" init -q
  git -C "$root/repos/myrepo" -c user.email=t@t -c user.name=t \
    -c commit.gpgsign=false commit -q --allow-empty -m init
  git -C "$root/repos/myrepo" worktree add -q "$root/repos/myrepo/.worktrees/wt"
  echo data > "$root/repos/sibling/file.txt"
  echo "=== git --git-common-dir from linked worktree ==="
  git -C "$root/repos/myrepo/.worktrees/wt" rev-parse \
    --path-format=absolute --git-common-dir
  echo "=== folio-perms read of sibling (expect exit 0) ==="
  # Must cd into the worktree: folio-perms reads $PWD, and bash recomputes
  # PWD to getcwd() at startup, so a bare `PWD=... ` prefix is ignored.
  ( cd "$root/repos/myrepo/.worktrees/wt" \
    && bash -x {{justfile_directory()}}/moxins/folio/bin/folio-perms \
      read "$root/repos/sibling/file.txt" )
  echo "exit=$?"

# Prototype: build the moxy Linux OCI image and run it via Apple `container`.
# Requires `container` installed + `container system start`. The image is the
# moxy binary alone (no moxins/maneater); `version` is the smoke test.
[group("explore")]
container-prototype:
  #!/usr/bin/env bash
  set -euo pipefail
  nix build .#moxy-oci-image
  container image load -i result
  container run --rm moxy:latest version
