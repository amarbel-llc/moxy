#!/usr/bin/env bash
set -euo pipefail

QUERY="${1:?usage: man-search-embed.bash <query> [top_k]}"
TOP_K="${2:-10}"
LLAMA_PORT="${LLAMA_PORT:-8922}"
LLAMA_URL="http://localhost:${LLAMA_PORT}/v1/embeddings"
INDEX_DIR="${HOME}/.local/share/moxy/man-index"

if ! curl -sf "http://localhost:${LLAMA_PORT}/health" >/dev/null 2>&1; then
  echo "error: llama-server not running on port $LLAMA_PORT" >&2
  echo "start it with: just man-search-server" >&2
  exit 1
fi

if [[ ! -f "$INDEX_DIR/embeddings.jsonl" ]] || [[ ! -f "$INDEX_DIR/pages.txt" ]]; then
  echo "error: index not found. Run: just man-search-index" >&2
  exit 1
fi

# Embed the query with nomic search prefix
query_embedding=$(curl -sf "$LLAMA_URL" \
  -H "Content-Type: application/json" \
  -d "$(jq -cn --arg q "search_query: $QUERY" '{input: $q, model: "nomic"}')" |
  jq -c '.data[0].embedding')

# Compute cosine similarity against all indexed embeddings and return top K
# Uses jq for the math — each line of embeddings.jsonl is a vector
paste -d $'\t' "$INDEX_DIR/pages.txt" "$INDEX_DIR/embeddings.jsonl" |
  jq -R --argjson q "$query_embedding" --argjson k "$TOP_K" '
    split("\t") | .[0] as $page | (.[1] | fromjson) as $vec |
    # cosine similarity: dot(q, vec) / (|q| * |vec|)
    ([$q, $vec] | transpose | map(.[0] * .[1]) | add) as $dot |
    ([$q | .[] | . * .] | add | sqrt) as $nq |
    ([$vec | .[] | . * .] | add | sqrt) as $nv |
    {page: $page, score: ($dot / ($nq * $nv))}
  ' |
  jq -s 'sort_by(-.score) | .[:$k]' --argjson k "$TOP_K" |
  jq -r '.[] | "\(.score | . * 1000 | round / 1000)\t\(.page)"'
