# A gws stand-in for the piers create/update tests (piers_*.bats). It logs
# each call's argv (tab-separated) to $GWS_STUB_LOG and keeps what it was
# given under $GWS_STUB_DIR:
#
#   doc-create.json        the new Doc's metadata (drive files create, no upload)
#   images/<n>             each uploaded image (answered as stub-image-<n>)
#   batch-update-<n>.json  the body of the n-th batchUpdate
#   comments-params.json   the params of a comments list
#   reply-params.json, reply.json   the params and body of a replies create
#
# `documents get` answers with $GWS_STUB_DIR/document.json when a test wrote
# one, else a one-tab Doc (t.0) with an empty body and a fresh footnote fn-1.
# batchUpdate replies give createFootnote requests ids fn-1, fn-2, …
#
# Failure switches: GWS_STUB_FAIL_PERMISSIONS, GWS_STUB_FAIL_BATCH_UPDATE
# (every batchUpdate call fails), GWS_STUB_FAIL_BATCH_UPDATE_AT=N (only the
# Nth batchUpdate call fails, 1-indexed — for a multi-chunk write, #471);
# GWS_STUB_FAIL_DELETE, GWS_STUB_FAIL_ON_STDERR (batchUpdate fails with the
# reason on stderr only, as gws does for auth errors); GWS_STUB_FAIL_SILENTLY
# (batchUpdate exits 1 with no stdout or stderr at all, as gws does when the
# process never ran, #471); GWS_STUB_UNFETCHABLE_ONCE (the first batchUpdate
# holding an image is refused as Docs refuses a not-yet-fetchable one);
# GWS_STUB_UNFETCHABLE_URI=<substring> (every image whose uri contains it is
# refused).

setup_gws_stub() {
  export GWS_STUB_DIR="$HOME/gws-stub-state"
  export GWS_STUB_LOG="$GWS_STUB_DIR/calls"
  export GWS_STUB_JQ
  GWS_STUB_JQ="$(command -v jq)"
  export GWS_BIN="$HOME/gws-stub"
  mkdir -p "$GWS_STUB_DIR/images"

  # No shebang — the nix sandbox lacks /usr/bin/env; zx runs it through bash.
  cat >"$GWS_BIN" <<'EOF'
set -euo pipefail
{ printf '%s\t' "$@"; printf '\n'; } >>"$GWS_STUB_LOG"
upload="" json="" params=""
args=("$@")
for ((i = 0; i < $#; i++)); do
  case "${args[i]}" in
  --upload) upload="${args[i + 1]}" ;;
  --json) json="${args[i + 1]}" ;;
  --params) params="${args[i + 1]}" ;;
  esac
done
refuse_image() { # index
  echo "{\"error\":{\"message\":\"Invalid requests[$1].insertInlineImage: There was a problem retrieving the image. The provided image should be publicly accessible.\"}}"
  exit 1
}
case "$1 $2 $3" in
"drive files create")
  if [ -n "$upload" ]; then
    n=$(($(ls "$GWS_STUB_DIR/images" | wc -l) + 1))
    cp "$upload" "$GWS_STUB_DIR/images/$n"
    echo "{\"id\":\"stub-image-$n\"}"
  else
    printf '%s' "$json" >"$GWS_STUB_DIR/doc-create.json"
    "$GWS_STUB_JQ" -c '{id: "stub-doc", kind: "drive#file", name, mimeType}' <<<"$json"
  fi
  ;;
"drive permissions create")
  if [ -n "${GWS_STUB_FAIL_PERMISSIONS:-}" ]; then
    echo '{"error":{"message":"stub sharing failure"}}'
    exit 1
  fi
  echo '{"id":"anyoneWithLink"}'
  ;;
"docs documents get")
  if [ -e "$GWS_STUB_DIR/document.json" ]; then
    cat "$GWS_STUB_DIR/document.json"
  else
    echo '{"documentId":"stub-doc","revisionId":"rev-1","tabs":[{"tabProperties":{"tabId":"t.0","title":"Tab 1"},"documentTab":{"body":{"content":[{"endIndex":1},{"startIndex":1,"endIndex":2,"paragraph":{}}]},"footnotes":{"fn-1":{"content":[{"startIndex":0,"endIndex":2,"paragraph":{}}]}}}}]}'
  fi
  ;;
"docs documents batchUpdate")
  # Counted with a glob: the piers wrapper's PATH has coreutils but no grep.
  shopt -s nullglob
  earlier=("$GWS_STUB_DIR"/batch-update-*.json)
  n=$((${#earlier[@]} + 1))
  printf '%s' "$json" >"$GWS_STUB_DIR/batch-update-$n.json"
  if [ -n "${GWS_STUB_FAIL_BATCH_UPDATE:-}" ]; then
    echo '{"error":{"message":"stub batchUpdate failure"}}'
    exit 1
  fi
  if [ -n "${GWS_STUB_FAIL_BATCH_UPDATE_AT:-}" ] && [ "$n" -eq "${GWS_STUB_FAIL_BATCH_UPDATE_AT}" ]; then
    echo "{\"error\":{\"message\":\"stub batchUpdate failure on call $n\"}}"
    exit 1
  fi
  if [ -n "${GWS_STUB_FAIL_ON_STDERR:-}" ]; then
    echo 'error[auth]: stub token expired' >&2
    exit 2
  fi
  if [ -n "${GWS_STUB_FAIL_SILENTLY:-}" ]; then
    exit 1
  fi
  if [ -n "${GWS_STUB_UNFETCHABLE_ONCE:-}" ] && [ ! -e "$GWS_STUB_DIR/refused-once" ]; then
    index=$("$GWS_STUB_JQ" '.requests | map(has("insertInlineImage")) | index(true) // empty' <<<"$json")
    if [ -n "$index" ]; then
      : >"$GWS_STUB_DIR/refused-once"
      refuse_image "$index"
    fi
  fi
  if [ -n "${GWS_STUB_UNFETCHABLE_URI:-}" ]; then
    index=$("$GWS_STUB_JQ" --arg u "$GWS_STUB_UNFETCHABLE_URI" \
      '.requests | map(.insertInlineImage.uri // "" | contains($u)) | index(true) // empty' <<<"$json")
    [ -n "$index" ] && refuse_image "$index"
  fi
  "$GWS_STUB_JQ" -c '{replies: [foreach .requests[] as $r (0;
      if $r.createFootnote then . + 1 else . end;
      if $r.createFootnote then {createFootnote: {footnoteId: "fn-\(.)"}} else {} end)]}' <<<"$json"
  ;;
"drive comments list")
  printf '%s' "$params" >"$GWS_STUB_DIR/comments-params.json"
  echo '{"comments":[{"id":"c1","content":"looks good"}]}'
  ;;
"drive replies create")
  printf '%s' "$params" >"$GWS_STUB_DIR/reply-params.json"
  printf '%s' "$json" >"$GWS_STUB_DIR/reply.json"
  echo '{"id":"r1"}'
  ;;
"drive files delete")
  if [ -n "${GWS_STUB_FAIL_DELETE:-}" ]; then
    echo '{"error":{"message":"stub delete failure"}}'
    exit 1
  fi
  echo '{}'
  ;;
esac
EOF
  chmod +x "$GWS_BIN"
}

# jq over the n-th batchUpdate body: batch 1 '.requests | length'
batch() { jq -r "$2" "$GWS_STUB_DIR/batch-update-$1.json"; }
batches() { ls "$GWS_STUB_DIR" | grep -c '^batch-update-' || true; }
calls() { grep -c "^$(printf "$1")" "$GWS_STUB_LOG" || true; }
# The text of every insertText in batch 1, one per line.
inserted_texts() { batch 1 '.requests[] | .insertText.text // empty'; }
# jq: does batch 1 style the inserted text `$text` (exactly) with a text style
# for which `$pred` holds? Ranges are read against the first insertText's
# text, at index 1 (ASCII only: jq slices by codepoint, Docs by UTF-16 unit).
styled_text() { # text jq-predicate-on-textStyle
  jq -e --arg text "$1" "
    first(.requests[] | .insertText.text // empty) as \$all
    | [.requests[] | select(.updateTextStyle and .updateTextStyle.fields != \"*\")
        | select(.updateTextStyle.textStyle | $2)
        | .updateTextStyle.range | \$all[.startIndex - 1 : .endIndex - 1]]
    | index(\$text) != null" "$GWS_STUB_DIR/batch-update-1.json" >/dev/null
}
styled_paragraph() { # text jq-predicate-on-paragraphStyle
  jq -e --arg text "$1" "
    (first(.requests[] | .insertText.text // empty) + \"\n\") as \$all
    | [.requests[] | select(.updateParagraphStyle)
        | select(.updateParagraphStyle.paragraphStyle | $2)
        | .updateParagraphStyle.range | \$all[.startIndex - 1 : .endIndex - 1]]
    | index(\$text) != null" "$GWS_STUB_DIR/batch-update-1.json" >/dev/null
}
