#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LLAMA_PORT="${LLAMA_PORT:-8922}"
LLAMA_URL="http://localhost:${LLAMA_PORT}/v1/embeddings"
INDEX_DIR="${HOME}/.local/share/maneater/man-index"
BATCH_SIZE="${BATCH_SIZE:-8}"
PARALLELISM="${PARALLELISM:-8}"
LIMIT="${1:-0}"

if ! curl -sf "http://localhost:${LLAMA_PORT}/health" >/dev/null 2>&1; then
  echo "error: llama-server not running on port $LLAMA_PORT" >&2
  echo "start it with: just man-search-start" >&2
  exit 1
fi

mkdir -p "$INDEX_DIR"

# Collect all section 1 man page names
all_names=$(apropos -s 1 . 2>/dev/null | sort -u | sed 's/([^)]*).*//; s/,.*//; s/ *$//' | sort -u)

if [[ $LIMIT -gt 0 ]]; then
  all_names=$(echo "$all_names" | head -"$LIMIT")
fi

total=$(echo "$all_names" | wc -l | tr -d ' ')

echo "Extracting synopses for $total man pages (parallelism=$PARALLELISM)..." >&2

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# Extract in parallel using external script
echo "$all_names" | tr '\n' '\0' |
  xargs -0 -P "$PARALLELISM" -I {} "$SCRIPT_DIR/man-extract-synopsis.bash" {} \
    >"$tmpdir/synopses.jsonl"

synopsis_count=$(wc -l <"$tmpdir/synopses.jsonl" | tr -d ' ')
echo "Extracted $synopsis_count synopses ($((total - synopsis_count)) skipped)" >&2

if [[ $synopsis_count -eq 0 ]]; then
  echo "error: no synopses extracted" >&2
  exit 1
fi

# Save page names (one per line, matching embedding order)
jq -r '.name' "$tmpdir/synopses.jsonl" >"$INDEX_DIR/pages.txt"

# Embed all synopses in batches
echo "Embedding $synopsis_count synopses..." >&2

split -l "$BATCH_SIZE" "$tmpdir/synopses.jsonl" "$tmpdir/batch-"

batch_count=$(ls "$tmpdir"/batch-* 2>/dev/null | wc -l | tr -d ' ')
echo "Processing $batch_count batches of $BATCH_SIZE..." >&2

>"$INDEX_DIR/embeddings.jsonl"
processed=0

for batch_file in "$tmpdir"/batch-*; do
  input_json=$(jq -s '[.[] | "search_document: " + .text]' "$batch_file")
  payload=$(jq -cn --argjson input "$input_json" '{input: $input, model: "nomic"}')

  if ! response=$(curl -sf --max-time 30 "$LLAMA_URL" \
    -H "Content-Type: application/json" \
    -d "$payload"); then
    echo "" >&2
    echo "error: embedding failed for batch $batch_file" >&2
    jq -r '.name' "$batch_file" >&2
    echo "payload size: $(echo "$payload" | wc -c | tr -d ' ') bytes" >&2
    exit 1
  fi

  echo "$response" | jq -c '.data[].embedding' >>"$INDEX_DIR/embeddings.jsonl"

  batch_size=$(jq -c '.' "$batch_file" | wc -l | tr -d ' ')
  processed=$((processed + batch_size))
  printf "\r  %d / %d" "$processed" "$synopsis_count" >&2
done

echo "" >&2

# Verify line counts match
embed_count=$(wc -l <"$INDEX_DIR/embeddings.jsonl" | tr -d ' ')
page_count=$(wc -l <"$INDEX_DIR/pages.txt" | tr -d ' ')

if [[ $embed_count != "$page_count" ]]; then
  echo "error: embedding count ($embed_count) != page count ($page_count)" >&2
  exit 1
fi

echo "Index saved to $INDEX_DIR ($embed_count entries)" >&2
echo "  pages.txt: page names" >&2
echo "  embeddings.jsonl: one embedding vector per line (from NAME+SYNOPSIS+DESCRIPTION)" >&2
