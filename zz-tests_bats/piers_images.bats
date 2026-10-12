#!/usr/bin/env bats

# bats file_tags=piers

# piers create/update place images in the Doc themselves (#449): each local
# image and each PlantUML diagram is taken out of the markdown (a placeholder
# token is uploaded in its place), hosted on Drive for a moment, and put back
# with the Docs API's insertInlineImage at an explicit display size. These
# tests run the nix-wrapped scripts with the real rsvg-convert and plantuml
# against a gws stub that records every call.

load 'common'

BIN="${PIERS_BIN:-$BATS_TEST_DIRNAME/../result-moxins/share/moxy/moxins/piers/bin}"

ONE_PIXEL_PNG_BASE64="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
ONE_PIXEL_GIF_BASE64="R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"

TOKEN='PIERSIMG[0-9A-F]{8}N[0-9]+E'

setup() {
  setup_test_home
  export GWS_STUB_DIR="$HOME/gws-stub-state"
  export GWS_STUB_LOG="$GWS_STUB_DIR/calls"
  export GWS_STUB_JQ
  GWS_STUB_JQ="$(command -v jq)"
  export GWS_BIN="$HOME/gws-stub"
  mkdir -p "$GWS_STUB_DIR/images"

  # Stands in for gws. Logs each call's argv (tab-separated) and keeps what it
  # was given: the markdown as upload.md, each image as images/<n>, the
  # batchUpdate body as batch-update.json. `documents get` answers with a
  # one-paragraph Doc whose text is the uploaded markdown, starting at index 1.
  # No shebang — the nix sandbox lacks /usr/bin/env; zx runs it through bash.
  cat >"$GWS_BIN" <<'EOF'
set -euo pipefail
{ printf '%s\t' "$@"; printf '\n'; } >>"$GWS_STUB_LOG"
upload="" type="" json=""
args=("$@")
for ((i = 0; i < $#; i++)); do
  case "${args[i]}" in
  --upload) upload="${args[i + 1]}" ;;
  --upload-content-type) type="${args[i + 1]}" ;;
  --json) json="${args[i + 1]}" ;;
  esac
done
case "$1 $2 $3" in
"drive files create" | "drive files update")
  if [ "$type" = text/markdown ]; then
    cp "$upload" "$GWS_STUB_DIR/upload.md"
    echo '{"id":"stub-doc"}'
  else
    n=$(($(ls "$GWS_STUB_DIR/images" | wc -l) + 1))
    cp "$upload" "$GWS_STUB_DIR/images/$n"
    echo "{\"id\":\"stub-image-$n\"}"
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
  "$GWS_STUB_JQ" -Rs \
    '{body:{content:[{paragraph:{elements:[{startIndex:1,textRun:{content:.}}]}}]}}' \
    "$GWS_STUB_DIR/upload.md"
  ;;
"docs documents batchUpdate")
  printf '%s' "$json" >"$GWS_STUB_DIR/batch-update.json"
  if [ -n "${GWS_STUB_FAIL_BATCH_UPDATE:-}" ]; then
    echo '{"error":{"message":"stub batchUpdate failure"}}'
    exit 1
  fi
  echo '{}'
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

  DOCS="$HOME/docs"
  mkdir -p "$DOCS/diagrams"
  base64 -d <<<"$ONE_PIXEL_PNG_BASE64" >"$DOCS/diagrams/pixel.png"
}

teardown() {
  teardown_test_home
}

write_svg() { # path width height
  cat >"$1" <<EOF
<svg xmlns="http://www.w3.org/2000/svg" width="$2" height="$3" viewBox="0 0 $2 $3">
  <rect width="$2" height="$3" fill="#e2e8f0"/>
  <text x="10" y="20" font-family="sans-serif" font-size="14">diagram</text>
</svg>
EOF
}

# Width in pixels of a PNG, from its IHDR chunk (big-endian u32 at byte 16).
png_width() {
  od -An -tu1 -j16 -N4 "$1" |
    awk '{ print (($1 * 256 + $2) * 256 + $3) * 256 + $4 }'
}

uploaded_markdown() { cat "$GWS_STUB_DIR/upload.md"; }
batch_update() { jq -r "$1" "$GWS_STUB_DIR/batch-update.json"; }
calls() { grep -c "^$(printf "$1")" "$GWS_STUB_LOG" || true; }

function piers_create_passes_local_reference_through_without_base_dir { # @test
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)'
  assert_success
  assert_output --partial 'stub-doc'
  run uploaded_markdown
  assert_output '![d](diagrams/pixel.png)'
  assert_equal "$(calls 'docs\tdocuments')" "0"
  assert_equal "$(calls 'drive\tpermissions')" "0"
}

function piers_create_places_local_png_where_it_stood { # @test
  run "$BIN/create" "Title" 'Before ![d](diagrams/pixel.png "t") after' "$DOCS"
  assert_success
  assert_output --partial 'stub-doc'

  # The markdown goes up with a token in the image's place; gws refuses an
  # --upload path outside its cwd, so it must be handed a bare name.
  run uploaded_markdown
  assert_output --regexp "^Before $TOKEN after\$"
  run grep -c -e $'--upload\tupload.md\t' "$GWS_STUB_LOG"
  assert_output "1"

  # The image is hosted, shared by link, inserted over the token, and deleted.
  run cmp "$GWS_STUB_DIR/images/1" "$DOCS/diagrams/pixel.png"
  assert_success
  assert_equal "$(calls 'drive\tpermissions\tcreate')" "1"
  assert_equal "$(batch_update '.requests[0].deleteContentRange.range | "\(.startIndex) \(.endIndex)"')" "8 27"
  assert_equal "$(batch_update '.requests[1].insertInlineImage.location.index')" "8"
  assert_equal "$(batch_update '.requests[1].insertInlineImage.uri')" \
    "https://drive.google.com/uc?export=download&id=stub-image-1"
  assert_equal "$(batch_update '.requests[1].insertInlineImage.objectSize | "\(.width.magnitude)\(.width.unit) \(.height.magnitude)\(.height.unit)"')" \
    "0.75PT 0.75PT"
  run grep -c $'^drive\tfiles\tdelete\t--params\t{"fileId":"stub-image-1"}' "$GWS_STUB_LOG"
  assert_output "1"
}

function piers_update_renders_svg_at_double_resolution_and_natural_size { # @test
  write_svg "$DOCS/diagrams/small.svg" 200 100
  run "$BIN/update" "doc-id" '![d](diagrams/small.svg)' "$DOCS"
  assert_success
  assert_equal "$(png_width "$GWS_STUB_DIR/images/1")" "400"
  assert_equal "$(batch_update '.requests[1].insertInlineImage.objectSize | "\(.width.magnitude) \(.height.magnitude)"')" "150 75"
  run grep -c $'^drive\tfiles\tupdate\t--params\t{"fileId":"doc-id"}' "$GWS_STUB_LOG"
  assert_output "1"
}

function piers_create_shows_wide_svg_at_page_width { # @test
  write_svg "$DOCS/diagrams/wide.svg" 1248 100
  run "$BIN/create" "Title" '![d](diagrams/wide.svg)' "$DOCS"
  assert_success
  assert_equal "$(png_width "$GWS_STUB_DIR/images/1")" "2496"
  assert_equal "$(batch_update '.requests[1].insertInlineImage.objectSize | "\(.width.magnitude) \(.height.magnitude)"')" "468 37.5"
}

function piers_create_sizes_gif_from_its_header { # @test
  base64 -d <<<"$ONE_PIXEL_GIF_BASE64" >"$DOCS/diagrams/pixel.gif"
  run "$BIN/create" "Title" '![d](diagrams/pixel.gif)' "$DOCS"
  assert_success
  assert_equal "$(batch_update '.requests[1].insertInlineImage.objectSize | "\(.width.magnitude) \(.height.magnitude)"')" "0.75 0.75"
  run grep -c -e $'--upload-content-type\timage/gif\t' "$GWS_STUB_LOG"
  assert_output "1"
}

function piers_create_renders_plantuml_fence_without_base_dir { # @test
  content=$'# Flow\n\n```plantuml\nAlice -> Bob: hello\n```\n\n```text\nAlice -> Bob: literal\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  run uploaded_markdown
  assert_line --regexp "^$TOKEN\$"
  refute_output --partial 'Alice -> Bob: hello'
  assert_line '```text'
  assert_line 'Alice -> Bob: literal'

  # Rendered at twice the resolution it is displayed at (0.75pt per pixel).
  width_px=$(png_width "$GWS_STUB_DIR/images/1")
  assert_equal "$(batch_update '.requests[1].insertInlineImage.objectSize.width.magnitude')" \
    "$(jq -n "$width_px / 2 * 0.75")"
}

function piers_create_renders_local_puml_file { # @test
  printf '@startuml\nAlice -> Bob: hello\n@enduml\n' >"$DOCS/diagrams/flow.puml"
  run "$BIN/create" "Title" '![flow](diagrams/flow.puml)' "$DOCS"
  assert_success
  run uploaded_markdown
  assert_output --regexp "^$TOKEN\$"
  [[ $(png_width "$GWS_STUB_DIR/images/1") -gt 100 ]]
}

function piers_create_shows_wide_plantuml_at_page_width { # @test
  content=$'```plantuml\nparticipant "A participant with a deliberately long name number one" as A\nparticipant "A participant with a deliberately long name number two" as B\nparticipant "A participant with a deliberately long name number three" as C\nA -> B: a message that is itself long enough to push things apart\nB -> C: a message that is itself long enough to push things apart\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  assert_equal "$(batch_update '.requests[1].insertInlineImage.objectSize.width.magnitude')" "468"
  [[ $(png_width "$GWS_STUB_DIR/images/1") -gt 1248 ]]
}

function piers_create_places_several_images_last_first { # @test
  run "$BIN/create" "Title" 'A ![a](diagrams/pixel.png) B ![b](diagrams/pixel.png)' "$DOCS"
  assert_success
  assert_equal "$(batch_update '.requests | length')" "4"
  assert_equal "$(batch_update '.requests[0].deleteContentRange.range.startIndex > .requests[2].deleteContentRange.range.startIndex')" "true"
  assert_equal "$(calls 'drive\tfiles\tdelete')" "2"
}

function piers_create_deletes_hosted_images_when_placing_fails { # @test
  export GWS_STUB_FAIL_BATCH_UPDATE=1
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)' "$DOCS"
  assert_failure
  assert_output --partial 'document stub-doc was written, but placing its images did not complete'
  assert_output --partial 'stub batchUpdate failure'
  refute_output --partial '"content"'
  assert_equal "$(calls 'drive\tfiles\tdelete')" "1"
}

function piers_create_deletes_hosted_image_when_sharing_it_fails { # @test
  export GWS_STUB_FAIL_PERMISSIONS=1
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)' "$DOCS"
  assert_failure
  assert_output --partial 'stub sharing failure'
  run grep -c $'^drive\tfiles\tdelete\t--params\t{"fileId":"stub-image-1"}' "$GWS_STUB_LOG"
  assert_output "1"
}

function piers_create_fails_naming_hosted_image_it_could_not_delete { # @test
  export GWS_STUB_FAIL_DELETE=1
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)' "$DOCS"
  assert_failure
  assert_output --partial 'Drive file(s) stub-image-1 are still shared by link'
  refute_output --partial '"content"'
  # Tried twice before giving up.
  assert_equal "$(calls 'drive\tfiles\tdelete')" "2"
}

# PlantUML must not be able to pull local files into a published Doc. The
# fragment would add a very wide participant; it must be refused or ignored.
function piers_create_does_not_let_plantuml_include_local_files { # @test
  long_name=$(printf 'X%.0s' {1..150})
  printf 'participant "%s" as Wide\n' "$long_name" >"$HOME/fragment.iuml"
  content=$'```plantuml\n!include '"$HOME"$'/fragment.iuml\nAlice -> Bob: hello\n```\n'
  run "$BIN/create" "Title" "$content"
  if [[ $status -eq 0 ]]; then
    [[ $(png_width "$GWS_STUB_DIR/images/1") -lt 800 ]]
  else
    assert [ ! -e "$GWS_STUB_LOG" ]
  fi
}

function piers_create_fails_on_plantuml_syntax_error_before_uploading { # @test
  content=$'```plantuml\nthis is ((( not plantuml\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_failure
  assert_output --partial 'plantuml exited'
  assert [ ! -e "$GWS_STUB_LOG" ]
}

function piers_create_fails_on_missing_image_before_uploading { # @test
  run "$BIN/create" "Title" '![d](diagrams/absent.png)' "$DOCS"
  assert_failure
  assert_output --partial "cannot read $DOCS/diagrams/absent.png"
  assert [ ! -e "$GWS_STUB_LOG" ]
}

function piers_create_fails_on_unsupported_image_type { # @test
  : >"$DOCS/diagrams/photo.webp"
  run "$BIN/create" "Title" '![d](diagrams/photo.webp)' "$DOCS"
  assert_failure
  assert_output --partial 'unsupported extension'
}

function piers_update_reads_content_path_and_resolves_images_beside_it { # @test
  printf '# Record\n\n![d](diagrams/pixel.png)\n' >"$DOCS/record.md"
  run "$BIN/update" "doc-id" "" "" "$DOCS/record.md"
  assert_success
  run uploaded_markdown
  assert_line --regexp "^$TOKEN\$"
  run cmp "$GWS_STUB_DIR/images/1" "$DOCS/diagrams/pixel.png"
  assert_success
}

function piers_create_rejects_content_together_with_content_path { # @test
  printf 'x\n' >"$DOCS/record.md"
  run "$BIN/create" "Title" "inline" "" "$DOCS/record.md"
  assert_failure
  assert_output --partial 'either content or content_path'
  assert [ ! -e "$GWS_STUB_LOG" ]
}

function piers_create_requires_content_or_content_path { # @test
  run "$BIN/create" "Title"
  assert_failure
  assert_output --partial 'one of content or content_path is required'
}

function piers_create_leaves_remote_and_code_references_untouched { # @test
  content=$'![r](https://example.com/a.png) ![a](<https://example.com/b.png>) ![p](//example.com/c.png)\n\nInline `![c](diagrams/absent.png)` code.\n\n```\n![f](diagrams/absent.png)\n```'
  run "$BIN/create" "Title" "$content" "$DOCS"
  assert_success
  run uploaded_markdown
  assert_output "$content"
  assert_equal "$(calls 'drive\tpermissions')" "0"
}
