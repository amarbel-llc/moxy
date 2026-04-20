export MOXIN_PATH := justfile_directory() / "result" / "share" / "moxy" / "moxins"

default: build test test-status-clean-env

dev: build-go
  zx bin/dev.mjs

build: build-go build-nix

build-go: generate build-moxins
  go build -o build/moxy ./cmd/moxy

build-moxins:
  nix build .#moxy-moxins

generate:
  go generate ./internal/config/

build-gomod2nix:
  gomod2nix

build-nix: build-gomod2nix
  nix build --show-trace

dir_build := "build"

test: test-go test-bats test-validate-mcp test-status

test-bats: build-go
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test

test-bats-file file: build-go
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test-targets {{file}}

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

# Bump moxyVersion in flake.nix to the given semver
bump-version new_version:
  #!/usr/bin/env bash
  set -euo pipefail
  current=$(grep 'moxyVersion = ' flake.nix | sed 's/.*"\(.*\)".*/\1/')
  if [[ "$current" == "{{new_version}}" ]]; then
    echo "already at {{new_version}}" >&2
    exit 0
  fi
  sed -i.bak 's/moxyVersion = "'"$current"'"/moxyVersion = "{{new_version}}"/' flake.nix && rm flake.nix.bak
  echo "$current → {{new_version}}"

# Create a signed git tag for the current moxyVersion and push it to origin
tag:
  #!/usr/bin/env bash
  set -euo pipefail
  version=$(grep 'moxyVersion = ' flake.nix | sed 's/.*"\(.*\)".*/\1/')
  tag="v${version}"
  if git rev-parse "$tag" >/dev/null 2>&1; then
    echo "tag $tag already exists" >&2
    exit 1
  fi
  git tag -s "$tag" -m "Release $tag"
  echo "created tag $tag"
  git push origin "$tag"
  echo "pushed tag $tag"

brew-build:
  nix build .#brew-tarball -o result-brew
  @echo "Tarball: $(ls result-brew/*.tar.gz)"

# Build brew tarball and publish a GitHub release for the given version
release-brew version:
  #!/usr/bin/env bash
  set -euo pipefail
  just brew-build
  gh release create "v{{version}}" \
    result-brew/*.tar.gz \
    --repo amarbel-llc/moxy \
    --title "v{{version}}" \
    --notes "Release v{{version}}"

# Full release: bump moxyVersion, commit, push branch, signed tag + push, brew tarball, GitHub release
release new_version:
  #!/usr/bin/env bash
  set -euo pipefail
  just bump-version {{new_version}}
  if ! git diff --quiet flake.nix; then
    git add flake.nix
    git commit -m "chore: release v{{new_version}}"
  fi
  git push
  just tag
  just release-brew {{new_version}}

clean: clean-build

clean-build:
  rm -rf result result-brew build/

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
