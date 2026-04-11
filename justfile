export MOXIN_PATH := justfile_directory() / "build" / "moxins"

default: build test

build: build-go build-nix

build-go: generate build-moxins
  go build -o build/moxy ./cmd/moxy

build-moxins:
  mkdir -p build/moxins
  cp -r moxins/*/ build/moxins/
  find build/moxins -name '*.toml' -exec sed -i "s|@LIBEXEC@|{{justfile_directory()}}/libexec|g" {} +
  chmod +x libexec/*

generate:
  go generate ./internal/config/

build-gomod2nix:
  gomod2nix

build-nix: build-gomod2nix
  nix build --show-trace

dir_build := "build"

test: test-go test-bats test-validate-mcp test-validate

test-bats: build-go
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test

test-bats-file file: build-go
  just --set bin_dir {{justfile_directory()}}/{{dir_build}} zz-tests_bats/test-targets {{file}}

test-go:
  MOXIN_PATH="" go vet ./...
  MOXIN_PATH="" go test ./... -v

test-validate: build-go
  {{justfile_directory()}}/{{dir_build}}/moxy validate

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

mcp-inspect := "npx @modelcontextprotocol/inspector --cli"

test-mcp: build-go
  #!/usr/bin/env nix
  #! nix shell nixpkgs#nodejs --command bash
  set -euo pipefail
  tools=$({{mcp-inspect}} --method tools/list {{justfile_directory()}}/{{dir_build}}/moxy serve mcp)
  echo "$tools" | jq .

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

clean: clean-build

clean-build:
  rm -rf result build/
