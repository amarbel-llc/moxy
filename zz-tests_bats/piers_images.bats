#!/usr/bin/env bats

# bats file_tags=piers

# piers create/update embed images in the markdown they upload (#449): local
# PNG/JPEG/GIF files become data: URIs, SVG files are rasterized, and PlantUML
# (fenced blocks and .puml files) is rendered. These tests run the nix-wrapped
# scripts with the real rsvg-convert and plantuml, against a gws stub that
# keeps a copy of the markdown it was asked to upload.

load 'common'

BIN="${PIERS_BIN:-$BATS_TEST_DIRNAME/../result-moxins/share/moxy/moxins/piers/bin}"

ONE_PIXEL_PNG_BASE64="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

setup() {
  setup_test_home
  export GWS_STUB_ARGV="$HOME/gws-argv"
  export GWS_STUB_UPLOAD="$HOME/gws-upload.md"
  export GWS_BIN="$HOME/gws-stub"
  # No shebang — the nix sandbox lacks /usr/bin/env; zx runs it through bash.
  cat >"$GWS_BIN" <<'EOF'
set -euo pipefail
printf '%s\n' "$@" >"$GWS_STUB_ARGV"
while [ $# -gt 0 ]; do
  if [ "$1" = "--upload" ]; then cp "$2" "$GWS_STUB_UPLOAD"; fi
  shift
done
echo '{"id":"stub-doc"}'
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

# Width in pixels of the first embedded PNG in the uploaded markdown, read
# from the PNG's IHDR chunk (big-endian u32 at byte 16).
uploaded_png_width() {
  grep -o 'data:image/png;base64,[A-Za-z0-9+/=]*' "$GWS_STUB_UPLOAD" | head -n1 |
    cut -d, -f2 | base64 -d | od -An -tu1 -j16 -N4 |
    awk '{ print (($1 * 256 + $2) * 256 + $3) * 256 + $4 }'
}

function piers_create_passes_local_reference_through_without_base_dir { # @test
  run "$BIN/create" "Title" '![d](diagrams/pixel.png)'
  assert_success
  run cat "$GWS_STUB_UPLOAD"
  assert_output '![d](diagrams/pixel.png)'
}

function piers_create_embeds_local_png_relative_to_base_dir { # @test
  run "$BIN/create" "Title" 'Before ![d](diagrams/pixel.png "t") after' "$DOCS"
  assert_success
  assert_output --partial 'stub-doc'
  run cat "$GWS_STUB_UPLOAD"
  assert_output "Before ![d](data:image/png;base64,$ONE_PIXEL_PNG_BASE64 \"t\") after"
  # gws refuses an --upload path outside its cwd, so it must get a bare name.
  run cat "$GWS_STUB_ARGV"
  assert_line 'create'
  assert_line 'upload.md'
  assert_line 'text/markdown'
}

function piers_update_rasterizes_local_svg_to_png { # @test
  write_svg "$DOCS/diagrams/small.svg" 200 100
  run "$BIN/update" "doc-id" '![d](diagrams/small.svg)' "$DOCS"
  assert_success
  run cat "$GWS_STUB_UPLOAD"
  assert_output --partial '![d](data:image/png;base64,iVBORw0KGgo'
  refute_output --partial 'data:image/svg'
  assert_equal "$(uploaded_png_width)" "200"
  run cat "$GWS_STUB_ARGV"
  assert_line 'update'
  assert_line '{"fileId":"doc-id"}'
}

function piers_create_scales_wide_svg_down_to_page_width { # @test
  write_svg "$DOCS/diagrams/wide.svg" 1248 100
  run "$BIN/create" "Title" '![d](diagrams/wide.svg)' "$DOCS"
  assert_success
  assert_equal "$(uploaded_png_width)" "624"
}

function piers_create_renders_plantuml_fence_without_base_dir { # @test
  content=$'# Flow\n\n```plantuml\nAlice -> Bob: hello\n```\n\n```text\nAlice -> Bob: literal\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  run cat "$GWS_STUB_UPLOAD"
  assert_line --partial '![PlantUML diagram](data:image/png;base64,iVBORw0KGgo'
  refute_output --partial 'Alice -> Bob: hello'
  assert_line '```text'
  assert_line 'Alice -> Bob: literal'
}

function piers_create_renders_local_puml_file { # @test
  printf '@startuml\nAlice -> Bob: hello\n@enduml\n' >"$DOCS/diagrams/flow.puml"
  run "$BIN/create" "Title" '![flow](diagrams/flow.puml)' "$DOCS"
  assert_success
  run cat "$GWS_STUB_UPLOAD"
  assert_output --partial '![flow](data:image/png;base64,iVBORw0KGgo'
}

function piers_create_scales_wide_plantuml_down_to_page_width { # @test
  content=$'```plantuml\nparticipant "A participant with a deliberately long name number one" as A\nparticipant "A participant with a deliberately long name number two" as B\nparticipant "A participant with a deliberately long name number three" as C\nA -> B: a message that is itself long enough to push things apart\nB -> C: a message that is itself long enough to push things apart\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  width=$(uploaded_png_width)
  [[ $width -le 624 && $width -gt 500 ]]
}

function piers_create_fails_on_plantuml_syntax_error_before_uploading { # @test
  content=$'```plantuml\nthis is ((( not plantuml\n```\n'
  run "$BIN/create" "Title" "$content"
  assert_failure
  assert_output --partial 'plantuml'
  assert [ ! -e "$GWS_STUB_ARGV" ]
}

function piers_create_fails_on_missing_image_before_uploading { # @test
  run "$BIN/create" "Title" '![d](diagrams/absent.png)' "$DOCS"
  assert_failure
  assert_output --partial "cannot read $DOCS/diagrams/absent.png"
  assert [ ! -e "$GWS_STUB_ARGV" ]
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
  run cat "$GWS_STUB_UPLOAD"
  assert_line "![d](data:image/png;base64,$ONE_PIXEL_PNG_BASE64)"
}

function piers_create_rejects_content_together_with_content_path { # @test
  printf 'x\n' >"$DOCS/record.md"
  run "$BIN/create" "Title" "inline" "" "$DOCS/record.md"
  assert_failure
  assert_output --partial 'either content or content_path'
  assert [ ! -e "$GWS_STUB_ARGV" ]
}

function piers_create_requires_content_or_content_path { # @test
  run "$BIN/create" "Title"
  assert_failure
  assert_output --partial 'one of content or content_path is required'
}

function piers_create_leaves_remote_and_code_references_untouched { # @test
  content=$'![r](https://example.com/a.png)\n\nInline `![c](diagrams/absent.png)` code.\n\n```\n![f](diagrams/absent.png)\n```'
  run "$BIN/create" "Title" "$content" "$DOCS"
  assert_success
  run cat "$GWS_STUB_UPLOAD"
  assert_output "$content"
}
