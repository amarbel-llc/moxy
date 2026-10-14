#!/usr/bin/env bats

# bats file_tags=piers

# piers' noun tools after #331: comment (list/reply/resolve/reopen) and tab
# (create/update/delete) replace eight single-purpose tools, and batch-update
# replaces the five micro-edit tools (insert-text, replace-text,
# delete-content-range, update-text-style, update-paragraph-style). These
# tests check the gws calls each subcommand makes, against the stub in
# piers_stub.bash.

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

stub_json() { jq -r "$2" "$GWS_STUB_DIR/$1"; }
reply_body() { jq -c . "$GWS_STUB_DIR/reply.json"; }

function piers_comment_list_asks_for_comments_with_replies { # @test
  run "$BIN/comment" list "file-1"
  assert_success
  assert_output --partial 'looks good'
  assert_equal "$(stub_json comments-params.json '"\(.fileId) \(.pageSize)"')" "file-1 100"
  assert_equal "$(stub_json comments-params.json '.fields | contains("replies(")')" "true"
}

function piers_comment_reply_posts_content_without_an_action { # @test
  run "$BIN/comment" reply "file-1" "c1" "thanks"
  assert_success
  assert_equal "$(stub_json reply-params.json '"\(.fileId) \(.commentId)"')" "file-1 c1"
  assert_equal "$(reply_body)" '{"content":"thanks"}'
}

function piers_comment_resolve_and_reopen_set_the_action { # @test
  run "$BIN/comment" resolve "file-1" "c1" "done"
  assert_success
  assert_equal "$(reply_body)" '{"action":"resolve","content":"done"}'
  run "$BIN/comment" reopen "file-1" "c1"
  assert_success
  assert_equal "$(reply_body)" '{"action":"reopen"}'
}

function piers_comment_reply_requires_comment_id_and_content { # @test
  run "$BIN/comment" reply "file-1" "" "thanks"
  assert_failure
  assert_output --partial 'comment reply needs comment_id'
  run "$BIN/comment" reply "file-1" "c1"
  assert_failure
  assert_output --partial 'comment reply needs content'
}

function piers_comment_rejects_an_unknown_subcommand { # @test
  run "$BIN/comment" delete "file-1" "c1"
  assert_failure
  assert_output --partial 'unknown comment subcommand delete'
}

function piers_tab_create_adds_a_titled_nested_tab { # @test
  run "$BIN/tab" create "doc-1" "" "Stories" "t.0" "2"
  assert_success
  assert_equal "$(batch 1 '.requests[0].addDocumentTab.tabProperties | "\(.title) \(.parentTabId) \(.index)"')" "Stories t.0 2"
}

function piers_tab_update_sets_only_the_given_properties { # @test
  run "$BIN/tab" update "doc-1" "t.1" "Renamed" "" "" "📝"
  assert_success
  assert_equal "$(batch 1 '.requests[0].updateDocumentTabProperties | "\(.fields) \(.tabProperties.tabId) \(.tabProperties.title)"')" \
    "title,iconEmoji t.1 Renamed"
  assert_equal "$(batch 1 '.requests[0].updateDocumentTabProperties.tabProperties | has("index")')" "false"
}

function piers_tab_update_requires_a_property { # @test
  run "$BIN/tab" update "doc-1" "t.1"
  assert_failure
  assert_output --partial 'tab update needs one of title, index or icon_emoji'
  assert_equal "$(batches)" "0"
}

function piers_tab_delete_removes_the_tab { # @test
  run "$BIN/tab" delete "doc-1" "t.1"
  assert_success
  assert_equal "$(batch 1 '.requests[0].deleteTab.tabId')" "t.1"
}

function piers_batch_update_pins_the_revision { # @test
  run "$BIN/batch-update" "doc-1" '[{"insertText":{"location":{"index":1},"text":"hi"}}]' "rev-3"
  assert_success
  assert_equal "$(batch 1 '"\(.writeControl.requiredRevisionId) \(.requests[0].insertText.text)"')" "rev-3 hi"
}

function piers_batch_update_reports_requests_that_are_not_json { # @test
  run "$BIN/batch-update" "doc-1" '[{"insertText":' "rev-3"
  assert_failure
  assert_output --partial 'requests is not JSON'
  assert_equal "$(batches)" "0"
}

function piers_batch_update_reports_a_gws_failure_in_one_line { # @test
  export GWS_STUB_FAIL_BATCH_UPDATE=1
  run "$BIN/batch-update" "doc-1" '[]' "rev-3"
  assert_failure
  assert_output --partial 'gws docs documents batchUpdate failed'
  assert_output --partial 'stub batchUpdate failure'
  refute_output --partial 'ProcessOutput'
}
