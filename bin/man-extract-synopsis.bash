#!/usr/bin/env bash
# Extract NAME+SYNOPSIS+DESCRIPTION from a single man page as JSON.
# Usage: man-extract-synopsis.bash <page-name>
# Outputs one JSON line: {"name": "...", "text": "..."} or nothing on failure.
set -euo pipefail

page="${1:?usage: man-extract-synopsis.bash <page>}"

source_path=$(man -w "$page" 2>/dev/null) || exit 0

# Convert to markdown. Try pandoc directly first (handles GNU man pages
# including gzipped), fall back to mandoc normalization for mdoc pages
# where pandoc alone produces broken output (no section headers).
read_source() {
  if [[ $1 == *.gz ]]; then zcat "$1"; else cat "$1"; fi
}

has_headers() {
  grep -q '^# ' <<<"$1"
}

markdown=$(read_source "$source_path" | pandoc -f man -t markdown 2>/dev/null) || true
if [[ -z $markdown ]] || ! has_headers "$markdown"; then
  markdown=$(read_source "$source_path" | mandoc -T man 2>/dev/null | pandoc -f man -t markdown 2>/dev/null) || exit 0
fi

if [[ -z $markdown ]] || ! has_headers "$markdown"; then exit 0; fi

synopsis=""
in_section=""
while IFS= read -r line; do
  if [[ $line =~ ^#[[:space:]] ]]; then
    header="${line#\# }"
    header=$(echo "$header" | tr '[:lower:]' '[:upper:]' | sed 's/^ *//; s/ *$//')
    if [[ $header == "NAME" ]] || [[ $header == "SYNOPSIS" ]] || [[ $header == "DESCRIPTION" ]]; then
      in_section="$header"
      continue
    else
      if [[ -n $in_section ]]; then break; fi
      in_section=""
      continue
    fi
  fi
  if [[ -n $in_section ]]; then
    synopsis+="$line"$'\n'
  fi
done <<<"$markdown"

if [[ -z $synopsis ]]; then exit 0; fi

synopsis="${synopsis:0:500}"
jq -cn --arg name "$page" --arg text "$synopsis" '{name: $name, text: $text}'
