#!/usr/bin/env bats

# bats file_tags=piers

# piers create/update turn markdown into Docs API batchUpdate requests with
# pandoc and a custom writer (docs-requests.lua), instead of Drive's markdown
# import: code blocks render as code (#457), update rewrites one tab and
# leaves the others alone (#456), and a real parser replaces the old
# line-and-regex fence scanner (#453). These tests read the requests sent to
# a gws stub (piers_stub.bash); zz-pocs/pandoc-docs-poc checks the same
# requests against the live API.

load 'common'
load 'piers_stub'

BIN="${PIERS_BIN:-$BATS_TEST_DIRNAME/../result-moxins/share/moxy/moxins/piers/bin}"

setup() {
  setup_test_home
  setup_gws_stub
}

teardown() {
  teardown_test_home
}

# A Doc with two tabs, t.0 and t.1, each with some old body text.
write_two_tab_document() {
  cat >"$GWS_STUB_DIR/document.json" <<'EOF'
{"documentId":"doc-id","revisionId":"rev-7","tabs":[
  {"tabProperties":{"tabId":"t.0","title":"Record"},
   "documentTab":{"body":{"content":[{"endIndex":1},{"startIndex":1,"endIndex":20,"paragraph":{}}]}}},
  {"tabProperties":{"tabId":"t.1","title":"Stories"},
   "documentTab":{"body":{"content":[{"endIndex":1},{"startIndex":1,"endIndex":30,"paragraph":{}}]}},
   "childTabs":[{"tabProperties":{"tabId":"t.2","title":"Nested"},
     "documentTab":{"body":{"content":[{"endIndex":1},{"startIndex":1,"endIndex":2,"paragraph":{}}]}}}]}
]}
EOF
}

# Every tabId named by any location or range in batch 1.
tab_ids_named() {
  batch 1 '[.. | objects | select(has("startIndex") or has("index")) | .tabId] | unique | .[]'
}

function piers_create_writes_through_the_docs_api_not_drive_import { # @test
  run "$BIN/create" "Title" $'# Heading one\n\nBody with **bold**.\n'
  assert_success
  assert_equal "$(calls 'drive\tfiles\tcreate')" "1"
  run grep -c -e '--upload' "$GWS_STUB_LOG"
  assert_output "0"
  assert_equal "$(batch 1 '.requests[0].insertText.text')" $'Heading one\nBody with bold.'
  styled_paragraph "Heading one" '.namedStyleType == "HEADING_1"'
  styled_text "bold" '.bold'
}

# #457: Drive's import turned each code line into its own unstyled paragraph.
function piers_create_renders_fenced_code_as_one_shaded_monospace_block { # @test
  run "$BIN/create" "Title" $'```sh\necho hi\n  two\n\tthree\n```\n'
  assert_success
  code=$'echo hi\v  two\v\tthree'
  styled_text "$code" '.weightedFontFamily.fontFamily == "Roboto Mono"'
  styled_paragraph "$code" '.shading.backgroundColor.color != null'
}

# #453: fences nested in list items or blockquotes, and a line that starts
# with an inline triple-backtick span, are what a line scanner gets wrong.
function piers_create_parses_nested_and_quoted_fences_and_inline_spans { # @test
  content=$'- item\n  - nested\n\n    ```toml\n    key = "value"\n    ```\n\n> ```\n> quoted fence\n> ```\n\n```inline span``` starts this line.\n'
  run "$BIN/create" "Title" "$content"
  assert_success
  styled_text 'key = "value"' '.weightedFontFamily'
  styled_text "quoted fence" '.weightedFontFamily'
  styled_text "inline span" '.weightedFontFamily'
  run inserted_texts
  assert_output --partial 'starts this line.'
}

# #456: update rewrites the named tab only — every request is scoped to it,
# and nothing touches the other tabs.
function piers_update_rewrites_only_the_named_tab { # @test
  write_two_tab_document
  run "$BIN/update" "doc-id" $'# New\n\nText.\n' "" "" "t.1"
  assert_success
  assert_output --partial '\"tabId\": \"t.1\"'
  assert_equal "$(batch 1 '.requests[0].deleteContentRange.range | "\(.startIndex) \(.endIndex) \(.tabId)"')" "1 29 t.1"
  run tab_ids_named
  assert_output "t.1"
  # Fails rather than overwrite edits made since piers read the Doc.
  assert_equal "$(batch 1 '.writeControl.requiredRevisionId')" "rev-7"
}

function piers_update_defaults_to_the_first_tab { # @test
  write_two_tab_document
  run "$BIN/update" "doc-id" 'Text.'
  assert_success
  run tab_ids_named
  assert_output "t.0"
}

function piers_update_finds_a_nested_tab { # @test
  write_two_tab_document
  run "$BIN/update" "doc-id" 'Text.' "" "" "t.2"
  assert_success
  run tab_ids_named
  assert_output "t.2"
  # An empty body has nothing to clear.
  assert_equal "$(batch 1 '[.requests[] | select(.deleteContentRange)] | length')" "0"
}

function piers_update_fails_on_an_unknown_tab_listing_the_tabs { # @test
  write_two_tab_document
  run "$BIN/update" "doc-id" 'Text.' "" "" "t.9"
  assert_failure
  assert_output --partial 'has no tab t.9'
  assert_output --partial 't.1 (Stories)'
  assert_equal "$(batches)" "0"
}

function piers_create_writes_footnote_content_in_a_second_batch { # @test
  run "$BIN/create" "Title" $'Note.[^1]\n\n[^1]: Foot *emph*.\n'
  assert_success
  run batch 1 '[.requests[] | keys[0]] | join(" ")'
  assert_output --partial 'createFootnote'
  # The stub's fresh footnote fn-1 holds " \n": content goes at index 1.
  assert_equal "$(batch 2 '.requests[0].insertText | "\(.text)|\(.location.segmentId)|\(.location.index)|\(.location.tabId)"')" \
    "Foot emph.|fn-1|1|t.0"
  assert_equal "$(batch 2 '[.requests[] | select(.updateTextStyle.textStyle.italic) | .updateTextStyle.range.segmentId] | unique | .[]')" "fn-1"
}

function piers_create_builds_tables_and_fills_their_cells { # @test
  run "$BIN/create" "Title" $'Before.\n\n| A | B |\n|:--|--:|\n| x | 2 |\n'
  assert_success
  assert_equal "$(batch 1 '[.requests[] | select(.insertTable) | .insertTable | "\(.rows)x\(.columns)"][0]')" "2x2"
  run batch 1 '.requests[] | .insertText.text // empty'
  assert_line "A"
  assert_line "B"
  assert_line "x"
  assert_line "2"
  assert_equal "$(batch 1 '[.requests[] | select(.updateTableCellStyle)] | length')" "1"
}

function piers_create_renders_task_lists_as_checkbox_lists { # @test
  run "$BIN/create" "Title" $'- [ ] todo\n- [x] done\n'
  assert_success
  assert_equal "$(batch 1 '[.requests[] | .createParagraphBullets.bulletPreset // empty] | .[]')" "BULLET_CHECKBOX"
  run inserted_texts
  refute_output --partial '☐'
  styled_text "done" '.strikethrough'
}

function piers_create_continues_list_numbering_across_a_code_block { # @test
  run "$BIN/create" "Title" $'1. one\n\n   ```\n   code\n   ```\n2. two\n'
  assert_success
  assert_equal "$(batch 1 '[.requests[] | select(.createParagraphBullets)] | length')" "1"
  assert_equal "$(batch 1 '[.requests[] | select(.deleteParagraphBullets)] | length > 1')" "true"
}
