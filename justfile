export MOXIN_PATH := justfile_directory() / "result" / "share" / "moxy" / "moxins"

default: build test test-status-clean-env

dev: build-go
  zx bin/dev.mjs

# Start moxy over Streamable HTTP, wait for /healthz, then launch interactive claude.
# Moxy is killed when claude exits.
serve-http port="8080": build-go
  zx bin/serve-http.mjs --port {{port}}

build: build-go build-nix

build-go: generate build-moxins
  go build -o build/moxy ./cmd/moxy

build-moxins:
  nix build .#moxy-moxins

build-release-tarball:
  nix build .#release-tarball --no-link --print-out-paths

generate:
  go generate ./internal/config/

build-gomod2nix:
  gomod2nix

build-nix: build-gomod2nix
  nix build --show-trace

dir_build := "build"

test: test-go test-bats test-validate-mcp test-status test-flake-check

test-bats: build-go
  export RELEASE_TARBALL_DIR=$(nix build .#release-tarball --no-link --print-out-paths) && \
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test

# Validates the flake's structural outputs (packages.* are derivations,
# devShells eval, etc). Runs last so the nix store cache is already warm
# from prior build steps; incremental cost is eval + moxy-static +
# release-tarball, both small rebuilds that hit the store cache on reruns.
test-flake-check:
  nix flake check

test-bats-file file: build-go
  export RELEASE_TARBALL_DIR=$(nix build .#release-tarball --no-link --print-out-paths) && \
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test-targets {{file}}

# End-to-end: verify claude -p can see and call moxy MCP tools.
# Requires: claude CLI on PATH and authenticated.
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
test-migrated-tools: build-moxins
  nix run nixpkgs#bun -- x zx bin/test-migrated-tools.mjs

# Smoke-test the locally-built hamster moxin (doc, src, mod-read, go-vet, go-build, go-mod)
test-hamster: build-moxins
  nix run nixpkgs#bun -- x zx bin/test-hamster.mjs

test-go:
  MOXIN_PATH="" go vet ./...
  MOXIN_PATH="" go test ./... -v

test-status: build-go
  {{justfile_directory()}}/{{dir_build}}/moxy status

# Verify the nix-built binary discovers system moxins without ambient env
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
  cd "$HOME/repo"
  purse-first validate-mcp {{justfile_directory()}}/{{dir_build}}/moxy serve mcp

# Bisect helper: build and validate MCP loading at current commit
# Usage: git bisect start HEAD <known-good> -- && git bisect run just bisect-validate
[group: 'debug']
bisect-validate: build-go
  #!/usr/bin/env bash
  set -euo pipefail
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT
  export HOME="$tmpdir/home"
  mkdir -p "$HOME/repo"
  export MOXIN_PATH="{{justfile_directory()}}/result/share/moxy/moxins"
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

test-mcp: build-go
  #!/usr/bin/env nix
  #! nix shell nixpkgs#nodejs --command bash
  set -euo pipefail
  tools=$({{mcp-inspect}} --method tools/list {{justfile_directory()}}/{{dir_build}}/moxy serve mcp)
  echo "$tools" | jq .

test-tarball-grit:
  #!/usr/bin/env bash
  set -euo pipefail
  cd "{{justfile_directory()}}"

  # Build the grit moxin tarball
  echo "=== Building grit standalone tarball ==="
  result=$(nix build .#standalone-moxin-tarballs.grit --print-out-paths 2>/dev/null)
  tarball=$(ls "$result"/grit-moxin-*.tar.gz | head -1)
  echo "Tarball: $tarball"
  echo ""

  # Step 1: Extract tarball
  echo "=== STEP 1: Extract tarball ==="
  tmpdir=$(mktemp -d)
  tar -xzf "$tarball" -C "$tmpdir"
  echo "Extracted to: $tmpdir"
  echo "ls $tmpdir/grit/:"
  ls "$tmpdir/grit/"
  echo ""
  echo "TMPDIR=$tmpdir"
  echo ""

  # Step 2: Test serve-moxin
  echo "=== STEP 2: Test serve-moxin ==="
  moxy="{{justfile_directory()}}/build/moxy"

  output=$(printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n' \
    | MOXIN_PATH="$tmpdir" "$moxy" serve-moxin grit 2>/dev/null)

  echo "First 2 JSON-RPC responses:"
  echo "$output" | head -2
  echo ""

  # Verification with jq
  echo "=== VERIFICATION ==="
  line1=$(echo "$output" | head -1)
  line2=$(echo "$output" | tail -1)

  # Check initialize response
  server_name=$(echo "$line1" | jq -r '.result.serverInfo.name' 2>/dev/null || echo "ERROR")
  server_version=$(echo "$line1" | jq -r '.result.serverInfo.version' 2>/dev/null || echo "ERROR")

  if [[ "$server_name" == "grit" ]]; then
    echo "✓ Initialize response: serverInfo.name = \"$server_name\""
    echo "  serverInfo.version = \"$server_version\""
  else
    echo "✗ Initialize response: serverInfo.name = \"$server_name\" (expected 'grit')"
    exit 1
  fi

  # Check tools/list response
  tool_count=$(echo "$line2" | jq '.result.tools | length' 2>/dev/null || echo "ERROR")
  if [[ "$tool_count" != "ERROR" ]] && [[ "$tool_count" -gt 0 ]]; then
    echo "✓ Tools/list response: $tool_count tools found"
    echo "$line2" | jq '.result.tools[0:3] | map(.name)' 2>/dev/null | sed 's/^/  - /'
  else
    echo "✗ Tools/list response: could not parse tools"
    exit 1
  fi

  # Cleanup
  rm -rf "$tmpdir"
  echo ""
  echo "✓ All checks passed!"

run-nix *ARGS:
  nix run . -- {{ARGS}}

update: update-go

update-go:
  env GOPROXY=direct go get -u -t ./...
  go mod tidy

man-list section="1":
  apropos -s {{section}} . 2>/dev/null | sort -u

man-count section="1":
  apropos -s {{section}} . 2>/dev/null | sort -u | wc -l

man-count-all:
  @for s in 1 2 3 4 5 6 7 8; do \
    count=$(apropos -s $s . 2>/dev/null | sort -u | wc -l | tr -d ' '); \
    printf "section %s: %s pages\n" "$s" "$count"; \
  done

man-search query section="1":
  apropos -s {{section}} {{query}} 2>/dev/null | sort -u

# Semantic man page search via embedding similarity
# Requires: llama-server running with embedding model (just man-search-server)
# Example: just man-search-embed "synchronize files"
man-search-embed query top_k="10":
  bin/man-search-embed.bash "{{query}}" "{{top_k}}"

# Build/refresh the embedding index for all section 1 man pages
# Pass limit to index only the first N pages (0 = all)
man-search-index limit="0":
  bin/man-search-index.bash "{{limit}}"

man_search_pidfile := env("HOME") / ".local/share/moxy/man-search.pid"
man_search_logfile := env("HOME") / ".local/share/moxy/man-search.log"
man_search_port := env("LLAMA_PORT", "8922")

# Start the embedding server in the background (idempotent)
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

man-search-health:
  curl -sf http://localhost:{{man_search_port}}/health | jq .

# Embed a single string and show the first 5 dimensions
man-search-test-embed text:
  #!/usr/bin/env bash
  set -euo pipefail
  curl -sf "http://localhost:{{man_search_port}}/v1/embeddings" \
    -H "Content-Type: application/json" \
    -d "$(jq -cn --arg t "{{text}}" '{input: $t, model: "nomic"}')" \
    | jq '{dim: (.data[0].embedding | length), first_5: (.data[0].embedding[:5])}'

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
bump-version new_version:
  #!/usr/bin/env bash
  set -euo pipefail
  current=$(sed -n 's/^MOXY_VERSION=//p' version.env)
  if [[ "$current" == "{{new_version}}" ]]; then
    echo "already at {{new_version}}" >&2
    exit 0
  fi
  sed -i.bak 's/^MOXY_VERSION=.*/MOXY_VERSION={{new_version}}/' version.env && rm version.env.bak
  echo "$current → {{new_version}}"

# Create a signed git tag for the current MOXY_VERSION and push it to origin
tag:
  #!/usr/bin/env bash
  set -euo pipefail
  version=$(sed -n 's/^MOXY_VERSION=//p' version.env)
  tag="v${version}"
  if git rev-parse "$tag" >/dev/null 2>&1; then
    echo "tag $tag already exists" >&2
    exit 1
  fi
  git tag -s "$tag" -m "Release $tag"
  echo "created tag $tag"
  git push origin "$tag"
  echo "pushed tag $tag"

# Bump MOXY_VERSION on master, commit, push master, signed tag + push (CI handles release artifacts on tag push). Must be run from the master branch.
release new_version:
  #!/usr/bin/env bash
  set -euo pipefail
  current_branch=$(git rev-parse --abbrev-ref HEAD)
  if [[ "$current_branch" != "master" ]]; then
    echo "just release must be run on master (currently on $current_branch)" >&2
    exit 1
  fi
  just bump-version {{new_version}}
  if ! git diff --quiet version.env; then
    git add version.env
    git commit -m "chore: release v{{new_version}}"
  fi
  git push origin master
  just tag

# Open a PR against amarbel-llc/homebrew-moxy setting Formula/moxy.rb to v$version (run after `just release` — v$version assets must exist on the GitHub release; HOMEBREW_TAP_DIR overrides the default .tmp/homebrew-moxy checkout)
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

clean: clean-build

clean-build:
  rm -rf result build/

# Run `bun install` at the repo root (refresh bun.lock for mkBunMoxin bundling)
[group('debug')]
debug-bun-install:
  cd {{justfile_directory()}} && bun install

# Regenerate bun.nix from bun.lock via nix-community/bun2nix
[group('debug')]
debug-bun2nix:
  cd {{justfile_directory()}} && nix run github:nix-community/bun2nix -- -o bun.nix

# Smoke-test arboretum-moxin outline against POC sample
[group('debug')]
debug-arboretum-smoke:
  {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/outline {{justfile_directory()}}/zz-pocs/outline-poc/samples/sample.go

# Smoke-test arboretum-moxin search against a small fixture
[group('debug')]
debug-arboretum-search-smoke:
  {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/search 'console.log($MSG)' {{justfile_directory()}}/.tmp/astgrep-smoke

# Smoke-test arboretum-moxin search against a small Go fixture (lang=go)
[group('debug')]
debug-arboretum-search-go-smoke:
  {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/search 'fmt.Println($X)' {{justfile_directory()}}/.tmp/astgrep-smoke go

# Smoke-test arboretum-moxin rewrite (apply) against a small Go fixture
[group('debug')]
debug-arboretum-rewrite-go-smoke:
  {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/rewrite 'fmt.Println($X)' 'log.Info($X)' {{justfile_directory()}}/.tmp/astgrep-smoke go '' false

# Smoke-test arboretum md-toc against a tiny markdown blob on stdin
[group('debug')]
debug-arboretum-md-toc-smoke:
  printf '# Hello\n\n## World\n\nbody\n\n## Again\n' | {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/md-toc

# Smoke-test arboretum md-section against a tiny markdown blob on stdin
[group('debug')]
debug-arboretum-md-section-smoke:
  printf '# Hello\n\n## World\n\nbody\n\n## Again\nmore\n' | {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/md-section World

# Smoke-test arboretum md-anchor against a tiny markdown blob on stdin
[group('debug')]
debug-arboretum-md-anchor-smoke:
  printf '<a name="x"></a>\n# X\nbody\n\n<a name="y"></a>\n# Y\nmore\n' | {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/md-anchor x

# Smoke-test arboretum-moxin rewrite (dry-run) against a small fixture
[group('debug')]
debug-arboretum-rewrite-smoke:
  {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/rewrite 'console.log($MSG)' 'logger.info($MSG)' {{justfile_directory()}}/.tmp/astgrep-smoke '' '' true

# Smoke-test arboretum-moxin rewrite (apply) against a small fixture
[group('debug')]
debug-arboretum-rewrite-apply-smoke:
  {{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/rewrite 'console.log($MSG)' 'logger.info($MSG)' {{justfile_directory()}}/.tmp/astgrep-smoke '' '' false

# Probe ast-grep's --update-all output streams independently
[group('debug')]
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
[group('debug')]
debug-arboretum-regen-goldens:
  #!/usr/bin/env bash
  set -euo pipefail
  bin={{justfile_directory()}}/result/share/moxy/moxins/arboretum/bin/outline
  fixtures={{justfile_directory()}}/zz-tests_bats/test-fixtures/arboretum
  for f in "$fixtures"/sample.*; do
    case "$f" in *.golden) continue;; esac
    name=$(basename "$f")
    "$bin" "$f" | sed "s|$f|samples/$name|" > "$f.golden"
    echo "wrote $f.golden"
  done

# Integration test for moxin discovery via a fresh temp workspace
test-moxin-loading:
  zx bin/test-moxin-loading.mjs

# Integration test for internal/stderrlog per-session logging + rotation flow.
test-stderrlog:
  zx bin/test-stderrlog.mjs

# Debug: look for OOM kills in kernel ring buffer (needs pkexec; user sasha not in adm/systemd-journal groups)
# SHELL is sanitized to /bin/bash since pkexec rejects SHELL values not in /etc/shells.
[group('debug')]
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

[group('explore')]
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

# Test install-moxin.bash extraction logic against local nix build artifacts.
# Replicates the script's extract steps without hitting GitHub or brew.
# Usage: just debug-install-moxin piers
[group('debug')]
debug-install-moxin name:
  #!/usr/bin/env bash
  set -euo pipefail
  release_path=$(nix build .#release-tarball --no-link --print-out-paths)
  moxin_path=$(nix build ".#standalone-moxin-tarballs.{{name}}" --no-link --print-out-paths)

  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in arm64|aarch64) arch="arm64" ;; x86_64) arch="amd64" ;; esac
  platform="${os}-${arch}"

  dest=$(mktemp -d)
  INSTALL_BIN="$dest/bin"
  INSTALL_SHARE="$dest/share/moxy/moxins"

  echo "=== Extracting moxy binary (same logic as install-moxin.bash) ==="
  mkdir -p "$INSTALL_BIN"
  tmp=$(mktemp -d)
  tar -xz -C "$tmp" < "$release_path/moxy-$platform.tar.gz"
  install -m 755 "$tmp/moxy/bin/moxy" "$INSTALL_BIN/moxy"
  rm -rf "$tmp"

  echo "=== Extracting {{name}} moxin ==="
  tmp=$(mktemp -d)
  tar -xz -C "$tmp" < "$moxin_path/{{name}}-moxin-$platform.tar.gz"
  mkdir -p "$INSTALL_SHARE"
  cp -r "$tmp"/* "$INSTALL_SHARE"/
  rm -rf "$tmp"

  echo ""
  echo "=== Installed tree ==="
  find "$dest" -type f | head -40
  echo ""

  if [[ -f "$INSTALL_BIN/moxy" && -x "$INSTALL_BIN/moxy" ]]; then
    echo "PASS: $INSTALL_BIN/moxy is an executable file"
    file "$INSTALL_BIN/moxy"
  else
    echo "FAIL: $INSTALL_BIN/moxy missing or not executable"
    ls -laR "$INSTALL_BIN/" 2>/dev/null || echo "(bin/ does not exist)"
    exit 1
  fi

  echo ""
  echo "=== Testing serve-moxin discovery ==="
  MOXIN_PATH="$INSTALL_SHARE" "$INSTALL_BIN/moxy" list-moxins 2>/dev/null \
    | grep -q "{{name}}" \
    && echo "PASS: {{name}} discovered via list-moxins" \
    || { echo "FAIL: {{name}} not found in list-moxins"; exit 1; }

  echo ""
  echo "=== Validating MCP protocol (purse-first validate-mcp) ==="
  start_ts=$(date +%s)
  MOXIN_PATH="$INSTALL_SHARE" purse-first validate-mcp \
    -- "$INSTALL_BIN/moxy" serve-moxin --name "{{name}}" \
    >/tmp/validate-mcp-stdout.log 2>/tmp/validate-mcp-stderr.log \
    && echo "PASS: MCP protocol validation" \
    || { end_ts=$(date +%s); elapsed=$((end_ts - start_ts)); \
         echo "FAIL: MCP protocol validation (${elapsed}s elapsed)"; \
         echo "--- stdout ---"; cat /tmp/validate-mcp-stdout.log; \
         echo "--- stderr ---"; cat /tmp/validate-mcp-stderr.log; \
         exit 1; }

  rm -rf "$dest"

# Test validate-mcp against serve-moxin with devshell-built binary.
# Usage: just debug-validate-serve-moxin piers
[group('debug')]
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

# Reproduce tools-not-appearing via claude -p with the nix-built moxy.
[group('explore')]
explore-claude-p: build-nix
  bin/explore-claude-p.bash "{{justfile_directory()}}"

# Build the dynamic-perms POC driver. POC scope only — not wired into main test.
[group('explore')]
poc-build-dynamic-perms:
  go build -o build/moxy-exporel-dynamic-perms ./cmd/moxy-exporel-dynamic-perms

# Run the dynamic-perms POC bats wrapper. Driver self-asserts.
[group('explore')]
poc-test-dynamic-perms: poc-build-dynamic-perms
  bats {{justfile_directory()}}/zz-bats_explore/dynamic_perms_poc.bats

# Enable impure-derivations + ca-derivations on the nix-daemon and restart it.
# Idempotent: re-running is a no-op if both features are already in nix.custom.conf.
# Mirrors amarbel-llc/eng#41's resolution but for Determinate Nix on Linux instead of darwin.
[group('debug')]
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
[group('debug')]
debug-nix-setting key:
  #!/usr/bin/env bash
  set -euo pipefail
  nix config show --json 2>/dev/null | jq --arg k '{{key}}' '.[$k] // .[($k | sub("^extra-"; ""))]'

# Smallest possible __impure derivation, exercise every flag override path.
[group('debug')]
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
[group('debug')]
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
[group('debug')]
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

