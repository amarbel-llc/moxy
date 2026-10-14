#!/usr/bin/env bats

# bats file_tags=piers

# piers create/update place images themselves (#449): the markdown's writer
# (docs-requests.lua) marks each local image and each PlantUML diagram, which
# piers renders, hosts on Drive for a moment, and inserts with the Docs API's
# insertInlineImage at an explicit display size. These tests run the
# nix-wrapped scripts with the real pandoc, rsvg-convert and plantuml against
# a gws stub that records every call (piers_stub.bash).

load 'common'
load 'piers_stub'

BIN="${PIERS_BIN:-$BATS_TEST_DIRNAME/../result-moxins/share/moxy/moxins/piers/bin}"

ONE_PIXEL_PNG_BASE64="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
ONE_PIXEL_GIF_BASE64="R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"

setup() {
  setup_test_home
  setup_gws_stub
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

images() { batch "${1:-1}" '[.requests[] | select(.insertInlineImage) | .insertInlineImage]'; }
image_size() { # n-th image of batch 1 -> "W H"
  batch 1 "[.requests[] | select(.insertInlineImage)][$1].insertInlineImage.objectSize | \"\(.width.magnitude) \(.height.magnitude)\""
}

function piers_create_shows_local_image_as_text_without_base_dir { # @test
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)'
  assert_success
  assert_output --partial 'stub-doc'
  assert_output --partial 'local image diagrams/pixel.png was not placed'
  run inserted_texts
  assert_line '[image: diagrams/pixel.png]'
  assert_equal "$(calls 'drive\tpermissions')" "0"
}

function piers_create_places_local_png_where_it_stood { # @test
  run "$BIN/create" "Title" 'Before ![d](diagrams/pixel.png "t") after' "$DOCS"
  assert_success
  assert_output --partial 'stub-doc'

  # An empty Doc is created, then written through the Docs API: no markdown
  # goes to Drive's importer.
  run jq -r '"\(.name) \(.mimeType)"' "$GWS_STUB_DIR/doc-create.json"
  assert_output "Title application/vnd.google-apps.document"
  assert_equal "$(calls 'drive\tfiles\tcreate\t--json\t.*text/markdown')" "0"

  # The text goes in without the image, which is inserted where it stood.
  assert_equal "$(batch 1 '.requests[0].insertText.text')" "Before  after"
  run cmp "$GWS_STUB_DIR/images/1" "$DOCS/diagrams/pixel.png"
  assert_success
  assert_equal "$(calls 'drive\tpermissions\tcreate')" "1"
  assert_equal "$(batch 1 '[.requests[] | select(.insertInlineImage)][0].insertInlineImage.location.index')" "8"
  assert_equal "$(batch 1 '[.requests[] | select(.insertInlineImage)][0].insertInlineImage.uri')" \
    "https://drive.google.com/uc?export=download&id=stub-image-1"
  assert_equal "$(image_size 0)" "0.75 0.75"
  run grep -c $'^drive\tfiles\tdelete\t--params\t{"fileId":"stub-image-1"}' "$GWS_STUB_LOG"
  assert_output "1"
}

function piers_update_renders_svg_at_double_resolution_and_natural_size { # @test
  write_svg "$DOCS/diagrams/small.svg" 200 100
  run "$BIN/update" "doc-id" '![d](diagrams/small.svg)' "$DOCS"
  assert_success
  assert_equal "$(png_width "$GWS_STUB_DIR/images/1")" "400"
  assert_equal "$(image_size 0)" "150 75"
}

function piers_create_shows_wide_svg_at_page_width { # @test
  write_svg "$DOCS/diagrams/wide.svg" 1248 100
  run "$BIN/create" "Title" '![d](diagrams/wide.svg)' "$DOCS"
  assert_success
  assert_equal "$(png_width "$GWS_STUB_DIR/images/1")" "2496"
  assert_equal "$(image_size 0)" "468 37.5"
}

function piers_create_sizes_gif_from_its_header { # @test
  base64 -d <<<"$ONE_PIXEL_GIF_BASE64" >"$DOCS/diagrams/pixel.gif"
  run "$BIN/create" "Title" '![d](diagrams/pixel.gif)' "$DOCS"
  assert_success
  assert_equal "$(image_size 0)" "0.75 0.75"
  run grep -c -e $'--upload-content-type\timage/gif' "$GWS_STUB_LOG"
  assert_output "1"
}

# A 3x2 (width x height) JPEG: SOI, then a SOF0 segment. Unequal sides catch
# a swapped width/height (#453).
function piers_create_sizes_jpeg_from_its_frame_header { # @test
  printf '\xff\xd8\xff\xc0\x00\x11\x08\x00\x02\x00\x03\x03' >"$DOCS/diagrams/photo.jpg"
  run "$BIN/create" "Title" '![d](diagrams/photo.jpg)' "$DOCS"
  assert_success
  assert_equal "$(image_size 0)" "2.25 1.5"
}

function piers_create_renders_plantuml_fence_without_base_dir { # @test
  content=$'# Flow\n\n```plantuml\nAlice -> Bob: hello\n```\n\n```text\nAlice -> Bob: literal\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  run inserted_texts
  refute_output --partial 'Alice -> Bob: hello'
  assert_output --partial 'Alice -> Bob: literal'

  # Rendered at twice the resolution it is displayed at (0.75pt per pixel).
  width_px=$(png_width "$GWS_STUB_DIR/images/1")
  assert_equal "$(image_size 0 | cut -d' ' -f1)" "$(jq -n "$width_px / 2 * 0.75")"
}

# -pipe concatenates the PNGs of several diagrams; each is rendered and
# placed on its own instead (#453).
function piers_create_places_each_diagram_of_a_multi_diagram_fence { # @test
  content=$'```plantuml\n@startuml\nAlice -> Bob: one\n@enduml\n@startuml\nCarol -> Dave: two\n@enduml\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  assert_equal "$(images | jq length)" "2"
  assert_equal "$(images | jq '[.[].location.index] | unique | length')" "1"
  assert [ -e "$GWS_STUB_DIR/images/2" ]
}

function piers_create_renders_local_puml_file { # @test
  printf '@startuml\nAlice -> Bob: hello\n@enduml\n' >"$DOCS/diagrams/flow.puml"
  run "$BIN/create" "Title" '![flow](diagrams/flow.puml)' "$DOCS"
  assert_success
  [[ $(png_width "$GWS_STUB_DIR/images/1") -gt 100 ]]
}

function piers_create_shows_wide_plantuml_at_page_width { # @test
  content=$'```plantuml\nparticipant "A participant with a deliberately long name number one" as A\nparticipant "A participant with a deliberately long name number two" as B\nparticipant "A participant with a deliberately long name number three" as C\nA -> B: a message that is itself long enough to push things apart\nB -> C: a message that is itself long enough to push things apart\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  assert_equal "$(image_size 0 | cut -d' ' -f1)" "468"
  [[ $(png_width "$GWS_STUB_DIR/images/1") -gt 1248 ]]
}

function piers_create_places_several_images_last_first { # @test
  run "$BIN/create" "Title" 'A ![a](diagrams/pixel.png) B ![b](diagrams/pixel.png)' "$DOCS"
  assert_success
  assert_equal "$(images | jq length)" "2"
  assert_equal "$(images | jq '.[0].location.index > .[1].location.index')" "true"
  assert_equal "$(calls 'drive\tfiles\tdelete')" "2"
}

function piers_create_deletes_hosted_images_when_writing_fails { # @test
  export GWS_STUB_FAIL_BATCH_UPDATE=1
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)' "$DOCS"
  assert_failure
  assert_output --partial 'writing document stub-doc did not complete'
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

# Docs fetches a just-shared image only after a moment (#453: the retry was
# untested).
function piers_create_retries_hosted_image_docs_cannot_fetch_yet { # @test
  export GWS_STUB_UNFETCHABLE_ONCE=1
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)' "$DOCS"
  assert_success
  refute_output --partial 'warnings'
  run ls "$GWS_STUB_DIR"
  assert_line 'batch-update-2.json'
}

function piers_create_shows_unfetchable_remote_image_as_text_and_warns { # @test
  export GWS_STUB_UNFETCHABLE_URI=example.com
  run "$BIN/create" "Title" 'See ![r](https://example.com/a.png) here.'
  assert_success
  assert_output --partial 'remote image https://example.com/a.png could not be fetched'
  run ls "$GWS_STUB_DIR"
  assert_line 'batch-update-2.json'
  assert_equal "$(batch 2 '[.requests[] | .insertText.text // empty] | index("[image: https://example.com/a.png]") != null')" "true"
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

function piers_create_fails_on_plantuml_syntax_error_before_any_call { # @test
  content=$'```plantuml\nthis is ((( not plantuml\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_failure
  assert_output --partial 'plantuml exited'
  assert [ ! -e "$GWS_STUB_LOG" ]
}

function piers_create_fails_on_missing_image_before_any_call { # @test
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
  run cmp "$GWS_STUB_DIR/images/1" "$DOCS/diagrams/pixel.png"
  assert_success
  assert_equal "$(images | jq length)" "1"
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

function piers_create_leaves_remote_and_code_references_alone { # @test
  content=$'![r](https://example.com/a.png)\n\nInline `![c](diagrams/absent.png)` code.\n\n```\n![f](diagrams/absent.png)\n```'
  run "$BIN/create" "Title" "$content" "$DOCS"
  assert_success
  assert_equal "$(images | jq -r '.[0].uri')" "https://example.com/a.png"
  assert_equal "$(images | jq '.[0].objectSize')" "null"
  run inserted_texts
  assert_output --partial '![c](diagrams/absent.png)'
  assert_output --partial '![f](diagrams/absent.png)'
  assert_equal "$(calls 'drive\tpermissions')" "0"
}
