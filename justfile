export MOXIN_PATH := justfile_directory() / "result-moxins" / "share" / "moxy" / "moxins"

default: lint build test test-status-clean-env

# Pre-build gate aggregate: treefmt formatting check + golangci-lint. A hard
# CI gate via `default`.
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

# Read-only formatting gate: build the sandboxed treefmt check derivation,
# which exits non-zero on any drift with no working-tree side effects.
# Write-mode counterpart: codemod-fmt-treefmt.
[group("pre-build")]
lint-fmt:
  #!/usr/bin/env bash
  set -euo pipefail
  system=$(nix eval --raw --impure --expr 'builtins.currentSystem')
  nix build --print-build-logs --no-link ".#checks.${system}.treefmt"

# Go static analysis via golangci-lint. Config: ./.golangci.yml. Hard gate via
# the `lint` aggregate. MOXIN_PATH is cleared so package loading matches the
# test-go environment.
[group("pre-build")]
lint-go:
  MOXIN_PATH="" golangci-lint run

dir_build := "build"

[group("post-build")]
test: test-go test-bats test-bats-net_cap test-validate-mcp test-status test-flake-check

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

# Smoke-test the locally-built hamster moxin (doc, src, mod-read, go-vet, go-build, go-mod)
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

# Semantic man page search via embedding similarity
# Requires: llama-server running with embedding model (just man-search-server)
# Example: just man-search-embed "synchronize files"
[group("inspection")]
man-search-embed query top_k="10":
  bin/man-search-embed.bash "{{query}}" "{{top_k}}"

# Build/refresh the embedding index for all section 1 man pages
# Pass limit to index only the first N pages (0 = all)
[group("operational")]
man-search-index limit="0":
  bin/man-search-index.bash "{{limit}}"

man_search_pidfile := env("HOME") / ".local/share/moxy/man-search.pid"
man_search_logfile := env("HOME") / ".local/share/moxy/man-search.log"
man_search_port := env("LLAMA_PORT", "8922")

# Start the embedding server in the background (idempotent)
[group("operational")]
man-search-start:
  #!/usr/bin/env bash
  set -euo pipefail
  pidfile="{{man_search_pidfile}}"
  logfile="{{man_search_logfile}}"
  port="{{man_search_port}}"
  model_dir="${HOME}/.local/share/moxy/models"
  model_path="$model_dir/nomic-embed-text-v1.5.Q8_0.gguf"
  if [[ ! -f "$model_path" ]]; then
    echo "Model not found. Run: just man-search-download-model" >&2
    exit 1
  fi
  # Already running?
  if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    echo "already running (pid $(cat "$pidfile"))" >&2
    exit 0
  fi
  mkdir -p "$(dirname "$pidfile")"
  llama-server \
    --model "$model_path" \
    --port "$port" \
    --ctx-size 8192 \
    --ubatch-size 2048 \
    --embeddings \
    > "$logfile" 2>&1 &
  echo "$!" > "$pidfile"
  # Wait for health
  for i in $(seq 1 30); do
    if curl -sf "http://localhost:${port}/health" >/dev/null 2>&1; then
      echo "started (pid $(cat "$pidfile"), port $port)"
      exit 0
    fi
    sleep 1
  done
  echo "error: server failed to start (check $logfile)" >&2
  cat "$logfile" | tail -5 >&2
  exit 1

[group("inspection")]
man-search-health:
  curl -sf http://localhost:{{man_search_port}}/health | jq .

# Embed a single string and show the first 5 dimensions
[group("inspection")]
man-search-test-embed text:
  #!/usr/bin/env bash
  set -euo pipefail
  curl -sf "http://localhost:{{man_search_port}}/v1/embeddings" \
    -H "Content-Type: application/json" \
    -d "$(jq -cn --arg t "{{text}}" '{input: $t, model: "nomic"}')" \
    | jq '{dim: (.data[0].embedding | length), first_5: (.data[0].embedding[:5])}'

[group("operational")]
man-search-stop:
  #!/usr/bin/env bash
  set -euo pipefail
  pidfile="{{man_search_pidfile}}"
  if [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null; then
    kill "$(cat "$pidfile")"
    rm -f "$pidfile"
    echo "stopped"
  else
    rm -f "$pidfile"
    echo "not running"
  fi

# Download nomic-embed-text-v1.5 embedding model (~140MB)
[group("operational")]
man-search-download-model:
  #!/usr/bin/env bash
  set -euo pipefail
  model_dir="${HOME}/.local/share/moxy/models"
  mkdir -p "$model_dir"
  model_path="$model_dir/nomic-embed-text-v1.5.Q8_0.gguf"
  if [[ -f "$model_path" ]]; then
    echo "Model already exists: $model_path"
  else
    echo "Downloading nomic-embed-text-v1.5 (Q8_0, ~140MB)..."
    curl -L -o "$model_path" \
      "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf"
    echo "Downloaded to: $model_path"
  fi

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

# Open a PR against amarbel-llc/homebrew-moxy setting Formula/moxy.rb to v$version (run after `just release` — v$version assets must exist on the GitHub release; HOMEBREW_TAP_DIR overrides the default .tmp/homebrew-moxy checkout)
[group("maintenance")]
bump-formula version:
  #!/usr/bin/env bash
  set -euo pipefail
  version="{{version}}"
  template="scripts/moxy.rb.template"
  tap_dir="${HOMEBREW_TAP_DIR:-.tmp/homebrew-moxy}"
  branch="bump-moxy-v${version}"

  [ -f "$template" ] || { echo "template not found: $template" >&2; exit 1; }

  # Verify both release assets are published (not just the release tag).
  for platform in darwin-arm64 linux-amd64; do
    if ! gh release view "v${version}" --repo amarbel-llc/moxy \
        --json assets --jq ".assets[].name" \
        | grep -qx "moxy-${platform}.tar.gz"; then
      echo "asset moxy-${platform}.tar.gz not published on v${version} — wait for CI?" >&2
      exit 1
    fi
  done

  # Fresh tap checkout on origin/master.
  if [ -d "$tap_dir/.git" ]; then
    git -C "$tap_dir" fetch origin
    git -C "$tap_dir" checkout master
    git -C "$tap_dir" reset --hard origin/master
  else
    mkdir -p "$(dirname "$tap_dir")"
    gh repo clone amarbel-llc/homebrew-moxy "$tap_dir"
  fi

  if git -C "$tap_dir" show-ref --quiet "refs/heads/${branch}"; then
    echo "branch ${branch} already exists in ${tap_dir} — delete it or pick a different version" >&2
    exit 1
  fi

  # Download tarballs and compute sha256s.
  workdir=$(mktemp -d)
  trap 'rm -rf "$workdir"' EXIT
  declare -A sha
  for platform in darwin-arm64 linux-amd64; do
    asset="moxy-${platform}.tar.gz"
    echo "downloading ${asset}..."
    gh release download "v${version}" \
      --repo amarbel-llc/moxy \
      --pattern "${asset}" \
      --dir "$workdir"
    sha["$platform"]=$(sha256sum "${workdir}/${asset}" | awk '{print $1}')
    echo "  sha256[${platform}]: ${sha[$platform]}"
  done

  formula="$tap_dir/Formula/moxy.rb"
  git -C "$tap_dir" checkout -b "$branch"

  # Render the template over the formula. Every run regenerates the full
  # file — no in-place edits, no structural drift.
  sed -e "s|@VERSION@|${version}|g" \
      -e "s|@SHA_DARWIN@|${sha[darwin-arm64]}|g" \
      -e "s|@SHA_LINUX@|${sha[linux-amd64]}|g" \
      "$template" > "$formula"

  if git -C "$tap_dir" diff --quiet Formula/moxy.rb; then
    echo "formula already matches v${version}; nothing to do." >&2
    exit 0
  fi

  echo
  echo "=== diff ==="
  git -C "$tap_dir" --no-pager diff Formula/moxy.rb
  echo "==========="

  git -C "$tap_dir" add Formula/moxy.rb
  git -C "$tap_dir" commit -m "moxy ${version}"
  git -C "$tap_dir" push -u origin "$branch"
  body=$(printf '%s\n\n%s\n' \
    "Bumps to [amarbel-llc/moxy@v${version}](https://github.com/amarbel-llc/moxy/releases/tag/v${version})." \
    "Regenerated from scripts/moxy.rb.template. Sha256s computed from the GitHub release assets. Opened by \`just bump-formula\`.")
  gh pr create \
    --repo amarbel-llc/homebrew-moxy \
    --title "moxy ${version}" \
    --body "$body" \
    --head "$branch" \
    --base master

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

# Run sisyphus Python unit tests (test_validate.py) using the nix-wrapped
# python3 from the sisyphus moxin (which has marklas + mistune available).
# Agent dev-loop: run after editing _validate.py or test_validate.py.
[group("debug")]
debug-sisyphus-py-tests: build-moxins
  #!/usr/bin/env bash
  set -euo pipefail
  moxin_dir="{{justfile_directory()}}/result-moxins/share/moxy/moxins/sisyphus"
  # The nix wrapper burns in the python path; extract it from the create-issue wrapper.
  py_bin=$(grep -o '/nix/store/[^:]*-python3[^/]*/bin' "$moxin_dir/bin/create-issue" | head -1)/python3
  "$py_bin" "{{justfile_directory()}}/moxins/sisyphus/lib/test_validate.py"

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

